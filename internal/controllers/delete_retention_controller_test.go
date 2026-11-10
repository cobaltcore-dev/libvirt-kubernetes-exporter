// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

// -- helpers ------------------------------------------------------------------

func newRetentionCtrl(c client.Client) *DeleteRetentionController {
	return &DeleteRetentionController{
		Client:          c,
		RetentionPeriod: 1 * time.Hour,
		Hostname:        "test-host",
	}
}

func retentionReq(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: k8stypes.NamespacedName{Name: name}}
}

// crdWithDeletedAt builds a LibvirtDomain with deletedAt set to the given time.
func crdWithDeletedAt(uuid string, deletedAt time.Time) *v1alpha1.LibvirtDomain {
	domain := domainFixture(uuid, "vm-retention")
	crd := liveCRD(domain, "uid-retention-"+uuid)
	crd.Status.State = types.StateGone.String()
	crd.Status.DeletedAt = deletedAt.UTC().Format(metav1.RFC3339Micro)
	return crd
}

// seedStatus writes a CRD's status via the status subresource on the fake client.
func seedStatus(t *testing.T, c client.Client, crd *v1alpha1.LibvirtDomain) {
	t.Helper()
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("seed status for %s: %v", crd.Name, err)
	}
}

// -- tests --------------------------------------------------------------------

// TestRetention_NotYetExpired verifies that a CRD whose retention window has not
// yet elapsed is kept and requeued for the remaining duration.
func TestRetention_NotYetExpired(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111101"
	crdName := "test-host-" + uuid
	const retention = 1 * time.Hour
	deletedAt := time.Now().Add(-30 * time.Minute) // deleted 30m ago, 30m remaining

	crd := crdWithDeletedAt(uuid, deletedAt)
	c := buildClient(crd)
	seedStatus(t, c, crd)

	r := newRetentionCtrl(c)
	result, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Should requeue for roughly the remaining 30 minutes.
	if result.RequeueAfter <= 0 {
		t.Errorf("RequeueAfter = %v, want > 0 (retention not yet expired)", result.RequeueAfter)
	}
	if result.RequeueAfter > retention {
		t.Errorf("RequeueAfter = %v, want <= %v", result.RequeueAfter, retention)
	}

	// CRD must still exist.
	existing := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, existing); err != nil {
		t.Errorf("CRD should still exist before retention expires: %v", err)
	}
}

// TestRetention_Expired verifies that a CRD whose retention window has elapsed
// is deleted.
func TestRetention_Expired(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111102"
	crdName := "test-host-" + uuid
	deletedAt := time.Now().Add(-2 * time.Hour) // deleted 2h ago, window has passed

	crd := crdWithDeletedAt(uuid, deletedAt)
	c := buildClient(crd)
	seedStatus(t, c, crd)

	r := newRetentionCtrl(c)
	result, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result after deletion, got RequeueAfter=%v", result.RequeueAfter)
	}

	// CRD must be gone.
	gone := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, gone); err == nil {
		t.Error("CRD should have been deleted after retention expired")
	}
}

// TestRetention_NoDeletedAt verifies that a CRD without deletedAt set is
// ignored — only deleted-marked CRDs are subject to retention.
func TestRetention_NoDeletedAt(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111103"
	crdName := "test-host-" + uuid

	domain := domainFixture(uuid, "vm-live")
	crd := liveCRD(domain, "uid-live")
	// No deletedAt — domain is still alive.
	c := buildClient(crd)

	r := newRetentionCtrl(c)
	result, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result for live CRD, got %+v", result)
	}

	// CRD must still exist.
	existing := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, existing); err != nil {
		t.Errorf("live CRD should not have been deleted: %v", err)
	}
}

// TestRetention_DeletedAtOnLiveDomainSkipped verifies that an inconsistent CRD
// with a stale deletedAt but non-gone state is treated as suspicious and kept.
func TestRetention_DeletedAtOnLiveDomainSkipped(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111107"
	crdName := "test-host-" + uuid

	domain := domainFixture(uuid, "vm-live-stale-deleted-at")
	crd := liveCRD(domain, "uid-live-stale-deleted-at")
	crd.Status.State = types.StateRunning.String()
	crd.Status.StateCode = int32(types.StateRunning)
	crd.Status.DeletedAt = time.Now().Add(-2 * time.Hour).UTC().Format(metav1.RFC3339Micro)

	c := buildClient(crd)
	seedStatus(t, c, crd)

	r := newRetentionCtrl(c)
	result, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result for inconsistent non-gone CRD, got %+v", result)
	}

	existing := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, existing); err != nil {
		t.Errorf("non-gone CRD with deletedAt should not have been deleted: %v", err)
	}
}

// TestRetention_AlreadyGone verifies that reconciling a CRD that no longer
// exists (already cleaned up) is a silent no-op.
func TestRetention_AlreadyGone(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111104"
	crdName := "test-host-" + uuid

	r := newRetentionCtrl(buildClient()) // empty store
	_, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile should not error on NotFound: %v", err)
	}
}

// TestRetention_BadTimestamp verifies that an unparsable deletedAt value
// causes Reconcile to return an error rather than silently skipping.
func TestRetention_BadTimestamp(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111105"
	crdName := "test-host-" + uuid

	domain := domainFixture(uuid, "vm-bad-ts")
	crd := liveCRD(domain, "uid-bad-ts")
	crd.Status.State = types.StateGone.String()
	crd.Status.StateCode = int32(types.StateGone)
	crd.Status.DeletedAt = "not-a-timestamp"

	c := buildClient(crd)
	seedStatus(t, c, crd)

	r := newRetentionCtrl(c)
	_, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err == nil {
		t.Fatal("expected error for unparsable deletedAt timestamp")
	}
}

// TestRetention_ExactBoundary verifies that a CRD deleted at exactly the
// retention boundary (remaining ≈ 0) is deleted rather than requeued.
func TestRetention_ExactBoundary(t *testing.T) {
	uuid := "ccccdddd-eeee-ffff-0000-111111111106"
	crdName := "test-host-" + uuid
	const retention = 1 * time.Hour
	// Deleted just over the retention period ago — clearly expired.
	deletedAt := time.Now().Add(-retention).Add(-time.Millisecond)

	crd := crdWithDeletedAt(uuid, deletedAt)
	c := buildClient(crd)
	seedStatus(t, c, crd)

	r := newRetentionCtrl(c)
	_, err := r.Reconcile(context.Background(), retentionReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	gone := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, gone); err == nil {
		t.Error("CRD should have been deleted at the retention boundary")
	}
}
