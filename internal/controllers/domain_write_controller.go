// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"sync"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrlevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	ctrlsource "sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/exporter"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var domainWriteLogger = logging.For("domain-write")

// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains/status,verbs=get;update;patch

// DomainWriteController reconciles types.Update events by writing LibvirtDomain CRDs.
// Heartbeat writes are handled separately by Exporter.RunHeartbeats.
type DomainWriteController struct {
	client.Client
	KnownStore exporter.DomainKnownStore
	Workers    int // number of concurrent reconcile workers; defaults to 1

	mu      sync.Mutex
	pending map[string]*types.Update // uuid → latest update (stale-drop)
}

func (r *DomainWriteController) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	uuid := req.Name

	r.mu.Lock()
	update, ok := r.pending[uuid]
	if ok {
		delete(r.pending, uuid)
	}
	r.mu.Unlock()

	if !ok {
		return ctrl.Result{}, nil
	}

	domainWriteLogger.Debug("reconciling domain write", "uuid", uuid, "type", update.Type, "state", update.Domain.Status.Value)

	if err := r.writeDomain(ctx, update.Domain); err != nil {
		domainWriteLogger.Error("domain write failed", "uuid", uuid, "type", update.Type, "state", update.Domain.Status.Value, "err", err)
		r.mu.Lock()
		if _, exists := r.pending[uuid]; !exists {
			r.pending[uuid] = update
		}
		r.mu.Unlock()
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DomainWriteController) writeDomain(ctx context.Context, domain *types.DomainState) error {
	name := domain.Host + "-" + domain.UUID

	if domain.IsGone() {
		return r.markDeleted(ctx, domain, name)
	}

	crd := v1alpha1.DomainToLibvirtDomain(domain)
	crd.Name = name

	status := crd.Status
	result, err := controllerutil.CreateOrPatch(ctx, r.Client, crd, func() error {
		crd.Status = status
		return nil
	})
	if err != nil {
		return fmt.Errorf("createOrPatch domain CRD: %w", err)
	}
	if result == controllerutil.OperationResultCreated {
		statusPatch := client.MergeFrom(crd.DeepCopy())
		crd.Status = status
		if err := r.Status().Patch(ctx, crd, statusPatch); err != nil {
			return fmt.Errorf("patch domain CRD status after create: %w", err)
		}
	}

	// Update knownDomains with the UID so heartbeat can set the owner ref.
	uid := string(crd.UID)
	if uid != "" && r.KnownStore != nil {
		r.KnownStore.SetKnownDomain(domain.UUID, uid)
	}

	domainWriteLogger.Info("domain CRD written", "name", name, "state", domain.Status.Value)
	return nil
}

func (r *DomainWriteController) markDeleted(ctx context.Context, domain *types.DomainState, name string) error {
	crd := &v1alpha1.LibvirtDomain{}
	if err := r.Get(ctx, k8stypes.NamespacedName{Name: name}, crd); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil // already gone
		}
		return err
	}

	statusPatch := client.MergeFrom(crd.DeepCopy())
	crd.Status.DeletedAt = domain.DeletedAt.UTC().Format(metav1.RFC3339Micro)
	crd.Status.State = types.StateGone.String()
	crd.Status.StateCode = int32(types.StateGone)

	if err := r.Status().Patch(ctx, crd, statusPatch); err != nil {
		return fmt.Errorf("mark domain CRD as deleted: %w", err)
	}

	domainWriteLogger.Info("marked domain CRD as deleted", "name", name)

	// Clear from known domains — heartbeat writes are no longer needed.
	if r.KnownStore != nil {
		r.KnownStore.InvalidateDomain(domain.UUID)
	}

	return nil
}

func (r *DomainWriteController) SetupWithManager(mgr ctrl.Manager, eventCh <-chan ctrlevent.TypedGenericEvent[*types.Update]) error {
	if r.pending == nil {
		r.pending = make(map[string]*types.Update)
	}
	workers := r.Workers
	if workers <= 0 {
		workers = 1
	}
	return builder.TypedControllerManagedBy[reconcile.Request](mgr).
		Named("domain-write").
		WithOptions(controller.TypedOptions[reconcile.Request]{
			MaxConcurrentReconciles: workers,
		}).
		WatchesRawSource(ctrlsource.TypedChannel[*types.Update, reconcile.Request](
			eventCh,
			handler.TypedFuncs[*types.Update, reconcile.Request]{
				GenericFunc: func(_ context.Context, e ctrlevent.TypedGenericEvent[*types.Update], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
					upd := e.Object
					uuid := upd.Domain.UUID
					r.mu.Lock()
					existing, hadPending := r.pending[uuid]
					r.pending[uuid] = upd
					r.mu.Unlock()
					if hadPending {
						domainWriteLogger.Debug("pending update overwritten", "uuid", uuid, "old_state", existing.Domain.Status.Value, "new_state", upd.Domain.Status.Value)
					}
					q.Add(reconcile.Request{NamespacedName: k8stypes.NamespacedName{Name: uuid}})
				},
			},
		)).
		Complete(r)
}
