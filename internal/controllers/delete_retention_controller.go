// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var deleteLogger = logging.For("delete-retention")

// DeleteRetentionController reconciles LibvirtDomain objects that have been marked
// as deleted, holding them for a configured retention window before physical removal.
type DeleteRetentionController struct {
	client.Client
	RetentionPeriod time.Duration
	Hostname        string
}

// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=kvm.cloud.sap,resources=libvirtdomains/status,verbs=get

func (r *DeleteRetentionController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if crdmeta.HostFromName(req.Name) != r.Hostname {
		return ctrl.Result{}, nil
	}

	var domain v1alpha1.LibvirtDomain
	if err := r.Get(ctx, req.NamespacedName, &domain); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if domain.Status.DeletedAt == "" {
		return ctrl.Result{}, nil
	}
	if domain.Status.State != types.StateGone.String() {
		deleteLogger.Warn("deletedAt set on non-gone domain, skipping retention delete", "name", req.Name, "state", domain.Status.State, "deletedAt", domain.Status.DeletedAt)
		return ctrl.Result{}, nil
	}

	deletedAt, err := time.Parse(time.RFC3339Nano, domain.Status.DeletedAt)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid deletedAt timestamp %q: %w", domain.Status.DeletedAt, err)
	}

	remaining := time.Until(deletedAt.Add(r.RetentionPeriod))
	if remaining > 0 {
		deleteLogger.Debug("retention not yet expired, requeuing", "name", req.Name, "remaining", remaining.Round(time.Second))
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	deleteLogger.Info("retention expired, deleting CRD", "name", req.Name, "deletedAt", domain.Status.DeletedAt)
	if err := r.Delete(ctx, &domain); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

func (r *DeleteRetentionController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LibvirtDomain{}).
		Named("delete-retention").
		Complete(r)
}
