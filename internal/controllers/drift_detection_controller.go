// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"libvirt.org/go/libvirtxml"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var driftLogger = logging.For("drift-detection")

const driftDetectionDelay = 2 * time.Second

// TrackerReader is the read-only interface the drift controller needs from the tracker.
type TrackerReader interface {
	Ready() bool
	GetDomain(uuid string) (*types.DomainState, bool)
}

// DomainInvalidator is called after the drift controller recreates a domain CRD
// so the heartbeat exporter picks up the new UID for the owner reference.
type DomainInvalidator interface {
	SetKnownDomain(uuid, uid string)
	InvalidateDomain(uuid string)
}

// DriftDetectionController reconciles LibvirtDomain objects and corrects drift
// between the observed K8s state and the operator's internal tracker state.
type DriftDetectionController struct {
	client.Client
	Tracker     TrackerReader
	Invalidator DomainInvalidator
	Hostname    string
}

// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains/status,verbs=get;update;patch

func (r *DriftDetectionController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// On startup, the tracker may not yet be seeded with the current state of the hypervisor.
	// Waiting for the first successful poll ensures that the tracker has a complete view of the current state before we start reconciling CRDs.
	if !r.Tracker.Ready() {
		driftLogger.Debug("tracker not yet seeded, requeuing", "name", req.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Skip CRDs that belong to a different host — in multi-host deployments all
	// hosts write to the same namespace, and this tracker only knows its own UUIDs.
	if crdmeta.HostFromName(req.Name) != r.Hostname {
		return ctrl.Result{}, nil
	}

	var domain v1alpha1.LibvirtDomain
	if err := r.Get(ctx, req.NamespacedName, &domain); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// CRD was deleted — check if tracker still knows about it.
		// If so, it was deleted externally (user error) and needs to be recreated.
		// We can't extract the UUID from a missing object, so we derive it from the name.
		uuid := crdmeta.UUIDFromName(req.Name)
		if uuid == "" {
			return ctrl.Result{}, nil
		}
		driftLogger.Debug("CRD deleted, checking tracker", "name", req.Name, "uuid", uuid)
		tracked, ok := r.Tracker.GetDomain(uuid)
		if !ok {
			// Not in tracker either — nothing to do.
			driftLogger.Debug("CRD deleted and not tracked, nothing to do", "name", req.Name, "uuid", uuid)
			return ctrl.Result{}, nil
		}
		if tracked.IsGone() {
			// Domain is gone from libvirt — CRD deletion is intentional, don't recreate.
			driftLogger.Debug("CRD deleted and domain gone from libvirt, nothing to do", "name", req.Name, "uuid", uuid)
			return ctrl.Result{}, nil
		}
		// Domain is new — give the domain-write controller time to create the CRD before
		// we conclude it was deleted externally and race to recreate it ourselves.
		if time.Since(tracked.FirstSeen) < driftDetectionDelay {
			driftLogger.Debug("domain is new, requeuing to allow domain-write to complete", "name", req.Name, "uuid", uuid)
			return ctrl.Result{RequeueAfter: driftDetectionDelay}, nil
		}
		driftLogger.Warn("CRD deleted externally, recreating", "name", req.Name, "uuid", uuid)
		restored := v1alpha1.DomainToLibvirtDomain(tracked)
		// Capture status before Create — K8s strips it from the object on create for status subresources.
		status := restored.Status
		if err := r.Create(ctx, restored); err != nil {
			if !k8serrors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
			// Another writer recreated it. Refresh the UID cache so heartbeats use
			// the owner reference of the object that won the race.
			existing := &v1alpha1.LibvirtDomain{}
			if err := r.Get(ctx, req.NamespacedName, existing); err != nil {
				return ctrl.Result{}, err
			}
			if r.Invalidator != nil {
				r.Invalidator.SetKnownDomain(uuid, string(existing.UID))
			}
			return ctrl.Result{}, nil
		}
		// Restore status after Create has populated ResourceVersion.
		statusPatch := client.MergeFrom(restored.DeepCopy())
		restored.Status = status
		if err := r.Status().Patch(ctx, restored, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		// Register the new UID so the heartbeat exporter can set the owner reference.
		if r.Invalidator != nil {
			r.Invalidator.SetKnownDomain(uuid, string(restored.UID))
		}
		return ctrl.Result{}, nil
	}

	// Skip drift correction for very recently created CRDs — the domain-write controller
	// writes spec and status in two separate calls, so the drift controller may see the
	// CRD before the status write completes. driftDetectionDelay covers API server latency
	// plus informer cache sync lag in congested clusters.
	if time.Since(domain.CreationTimestamp.Time) < driftDetectionDelay {
		return ctrl.Result{RequeueAfter: driftDetectionDelay}, nil
	}

	// CRD exists — derive UUID from the immutable object name rather than labels,
	// which a user could modify.
	uuid := crdmeta.UUIDFromName(req.Name)
	if uuid == "" {
		driftLogger.Warn("CRD name does not contain a UUID, skipping", "name", req.Name)
		return ctrl.Result{}, nil
	}

	tracked, ok := r.Tracker.GetDomain(uuid)
	if !ok {
		// Tracker doesn't know this UUID after the first poll — the domain is gone
		// or was never seen. Set state to gone, start the retention clock if not already
		// set, and mark as potentially corrupted so consumers know the data may be stale.
		needsPatch := false
		patch := client.MergeFrom(domain.DeepCopy())

		if domain.Status.State != types.StateGone.String() {
			driftLogger.Warn("CRD has no tracked domain, setting state to gone", "name", req.Name, "uuid", uuid, "previous_state", domain.Status.State)
			domain.Status.State = types.StateGone.String()
			domain.Status.StateCode = int32(types.StateGone)
			needsPatch = true
		}
		if domain.Status.DeletedAt == "" {
			driftLogger.Warn("CRD has no tracked domain, setting deletedAt", "name", req.Name, "uuid", uuid)
			domain.Status.DeletedAt = time.Now().UTC().Format(metav1.RFC3339Micro)
			needsPatch = true
		} else if _, err := time.Parse(time.RFC3339Nano, domain.Status.DeletedAt); err != nil {
			driftLogger.Warn("CRD has invalid deletedAt, resetting", "name", req.Name, "uuid", uuid, "deletedAt", domain.Status.DeletedAt, "err", err)
			domain.Status.DeletedAt = time.Now().UTC().Format(metav1.RFC3339Micro)
			needsPatch = true
		}
		if !domain.Status.PotentiallyCorrupted {
			driftLogger.Warn("CRD has no tracked domain, marking as potentially corrupted", "name", req.Name, "uuid", uuid)
			domain.Status.PotentiallyCorrupted = true
			needsPatch = true
		}
		if needsPatch {
			if err := r.Status().Patch(ctx, &domain, patch); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	expected := v1alpha1.DomainToLibvirtDomain(tracked)

	// Correct label drift — labels are mutable but used for selection and cleanup.
	if !maps.Equal(domain.Labels, expected.Labels) {
		driftLogger.Warn("label drift detected, correcting", "name", req.Name, "uuid", uuid)
		labelPatch := client.MergeFrom(domain.DeepCopy())
		domain.Labels = expected.Labels
		if err := r.Patch(ctx, &domain, labelPatch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Compare the CRD status against what the tracker holds.
	// LastChanged and DomainXML are excluded — LastChanged is operator-managed and
	// changes on every state update; DomainXML is covered by XMLHash.
	if statusEqual(domain.Status, expected.Status) {
		return ctrl.Result{}, nil
	}

	driftLogger.Warn("drift detected, correcting", "name", req.Name, "uuid", uuid)
	statusPatch := client.MergeFrom(domain.DeepCopy())
	domain.Status = expected.Status
	if err := r.Status().Patch(ctx, &domain, statusPatch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DriftDetectionController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LibvirtDomain{}).
		Named("drift-detection").
		Complete(r)
}

// statusEqual compares the user-visible fields of LibvirtDomainStatus.
// LastChanged is excluded as it is operator-managed and changes on every update.
// DeletedAt is excluded because the tracker has no concept of a gone domain's deletion
// timestamp — it only knows whether the domain is alive or not. Drift on DeletedAt
// cannot be corrected from tracker state, so we do not detect it here.
// DomainXML is compared by unmarshaling both sides into any and using reflect.DeepEqual,
// which is key-order independent and handles the non-deterministic JSON marshaling of libvirtxml.
func statusEqual(a, b v1alpha1.LibvirtDomainStatus) bool {
	if a.UUID != b.UUID ||
		a.Name != b.Name ||
		a.Host != b.Host ||
		a.NovaMetadata != b.NovaMetadata ||
		a.State != b.State ||
		a.StateCode != b.StateCode ||
		a.StateReason != b.StateReason ||
		a.StateReasonCode != b.StateReasonCode ||
		a.MaxMemoryKB != b.MaxMemoryKB ||
		a.MemoryKB != b.MemoryKB ||
		a.VCPUs != b.VCPUs ||
		a.XMLHash != b.XMLHash ||
		a.PotentiallyCorrupted != b.PotentiallyCorrupted {
		return false
	}
	return domainXMLEqual(a, b, a.UUID)
}

func domainXMLEqual(a, b v1alpha1.LibvirtDomainStatus, uuid string) bool {
	aIsNil := a.DomainXML == nil
	bIsNil := b.DomainXML == nil
	if aIsNil != bIsNil {
		return false
	}
	if aIsNil {
		return true
	}
	// Unmarshal both into the same typed struct so null-vs-missing and key-ordering
	// differences are normalised before comparison.
	var aDom, bDom libvirtxml.Domain
	if err := json.Unmarshal(a.DomainXML.Raw, &aDom); err != nil {
		driftLogger.Warn("current DomainXML unparsable, skipping XML comparison", "uuid", uuid, "err", err)
		return true // current side unparsable — can't detect changes, skip
	}
	if err := json.Unmarshal(b.DomainXML.Raw, &bDom); err != nil {
		driftLogger.Warn("expected DomainXML unparsable, skipping XML comparison", "uuid", uuid, "err", err)
		return true // expected side unparsable — can't detect changes, skip
	}
	return reflect.DeepEqual(aDom, bDom)
}
