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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

// -- fakeTracker --------------------------------------------------------------

type fakeTracker struct {
	ready   bool
	domains map[string]*types.DomainState
}

func (f *fakeTracker) Ready() bool { return f.ready }
func (f *fakeTracker) GetDomain(uuid string) (*types.DomainState, bool) {
	d, ok := f.domains[uuid]
	return d, ok
}

// -- fakeInvalidator ----------------------------------------------------------

type fakeInvalidator struct {
	set         map[string]string
	invalidated []string
}

func newFakeInvalidator() *fakeInvalidator {
	return &fakeInvalidator{set: make(map[string]string)}
}

func (f *fakeInvalidator) SetKnownDomain(uuid, uid string) { f.set[uuid] = uid }
func (f *fakeInvalidator) InvalidateDomain(uuid string) {
	f.invalidated = append(f.invalidated, uuid)
}

// -- helpers ------------------------------------------------------------------

func newDriftCtrl(c client.Client, tracker *fakeTracker, inv *fakeInvalidator) *DriftDetectionController {
	return &DriftDetectionController{
		Client:      c,
		Tracker:     tracker,
		Invalidator: inv,
		Hostname:    "test-host",
	}
}

func driftReq(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: k8stypes.NamespacedName{Name: name}}
}

// liveCRD builds a LibvirtDomain with a known UID and a recent CreationTimestamp
// old enough to pass the driftDetectionDelay guard (default 2s).
func liveCRD(domain *types.DomainState, uid string) *v1alpha1.LibvirtDomain {
	crd := v1alpha1.DomainToLibvirtDomain(domain)
	crd.UID = k8stypes.UID(uid)
	crd.CreationTimestamp = metav1.Time{Time: time.Now().Add(-10 * time.Second)}
	return crd
}

// -- tests --------------------------------------------------------------------

// TestDrift_TrackerNotReady verifies that when the tracker hasn't completed its
// first poll, reconcile requeues without touching the API server.
func TestDrift_TrackerNotReady(t *testing.T) {
	tracker := &fakeTracker{ready: false}
	inv := newFakeInvalidator()
	c := buildClient()
	r := newDriftCtrl(c, tracker, inv)

	result, err := r.Reconcile(context.Background(), driftReq("test-host-aaaabbbb-cccc-dddd-eeee-ffffffffffff"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("RequeueAfter = %v, want 5s", result.RequeueAfter)
	}
}

// TestDrift_CRDMissing_DomainTracked_New verifies that when the CRD is missing
// but the domain was seen recently, reconcile requeues with a delay to let the
// domain-write controller create it first (avoids a write race).
func TestDrift_CRDMissing_DomainTracked_New(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000001"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-new")
	domain.FirstSeen = time.Now() // brand new — within driftDetectionDelay

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(buildClient(), tracker, inv) // no CRD in store

	result, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != driftDetectionDelay {
		t.Errorf("RequeueAfter = %v, want %v (grace period for new domain)", result.RequeueAfter, driftDetectionDelay)
	}
}

// TestDrift_CRDMissing_DomainTracked_Old verifies that when the CRD is missing
// and the domain was seen long enough ago, the drift controller recreates the CRD
// and registers the new UID with the invalidator.
func TestDrift_CRDMissing_DomainTracked_Old(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000002"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-old")
	domain.FirstSeen = time.Now().Add(-1 * time.Minute) // old enough

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(buildClient(), tracker, inv) // no CRD in store

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// CRD should now exist.
	crd := &v1alpha1.LibvirtDomain{}
	if err := r.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, crd); err != nil {
		t.Fatalf("CRD not recreated: %v", err)
	}
	// Invalidator should have been called with the new UID.
	if _, ok := inv.set[uuid]; !ok {
		t.Error("SetKnownDomain not called after CRD recreation")
	}
}

// TestDrift_CRDMissing_DomainGone verifies that when the CRD is missing and
// the domain is gone (DeletedAt set), no recreation occurs — the deletion was
// intentional.
func TestDrift_CRDMissing_DomainGone(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000003"
	crdName := "test-host-" + uuid
	domain := goneDomain(uuid, "vm-gone")

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(buildClient(), tracker, inv)

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	crd := &v1alpha1.LibvirtDomain{}
	if err := r.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, crd); err == nil {
		t.Error("CRD should not have been recreated for a gone domain")
	}
	if len(inv.set) > 0 {
		t.Error("SetKnownDomain should not be called for a gone domain")
	}
}

// TestDrift_CRDMissing_NotTracked verifies that when both the CRD and the
// tracker entry are absent, reconcile is a no-op.
func TestDrift_CRDMissing_NotTracked(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000004"
	crdName := "test-host-" + uuid

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(buildClient(), tracker, inv)

	result, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result for not-tracked missing CRD, got %+v", result)
	}
}

// TestDrift_NewCRDSkipped verifies that a CRD created very recently is skipped
// to allow the domain-write controller to finish its status write.
func TestDrift_NewCRDSkipped(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000005"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-brand-new")

	// CRD exists but was just created.
	crd := v1alpha1.DomainToLibvirtDomain(domain)
	crd.CreationTimestamp = metav1.Time{Time: time.Now()} // just now

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(buildClient(crd), tracker, inv)

	result, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != driftDetectionDelay {
		t.Errorf("RequeueAfter = %v, want %v (grace period for new CRD)", result.RequeueAfter, driftDetectionDelay)
	}
}

// TestDrift_LabelDrift verifies that when a CRD's labels diverge from what the
// tracker holds, the labels are patched back to the expected values.
func TestDrift_LabelDrift(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000006"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-label-drift")

	crd := liveCRD(domain, "uid-label-drift")
	// Tamper with the labels to simulate external drift.
	crd.Labels[crdmeta.LabelName()] = "tampered-name"

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	c := buildClient(crd)
	r := newDriftCtrl(c, tracker, inv)

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, updated); err != nil {
		t.Fatalf("Get after label patch: %v", err)
	}
	if updated.Labels[crdmeta.LabelName()] != "vm-label-drift" {
		t.Errorf("label %q = %q, want vm-label-drift", crdmeta.LabelName(), updated.Labels[crdmeta.LabelName()])
	}
}

// TestDrift_StatusDrift verifies that when a CRD's status diverges from the
// tracker state, the status is patched back.
func TestDrift_StatusDrift(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000007"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-status-drift")

	crd := liveCRD(domain, "uid-status-drift")
	c := buildClient(crd)
	// Tamper with the stored status to simulate drift.
	tamperedCRD := crd.DeepCopy()
	tamperedCRD.Status.State = "tampered"
	tamperedCRD.Status.StateCode = 99
	if err := c.Status().Update(context.Background(), tamperedCRD); err != nil {
		t.Fatalf("seed tampered status: %v", err)
	}

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	inv := newFakeInvalidator()
	r := newDriftCtrl(c, tracker, inv)

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, updated); err != nil {
		t.Fatalf("Get after status patch: %v", err)
	}
	if updated.Status.State != "running" {
		t.Errorf("status.state = %q, want running after drift correction", updated.Status.State)
	}
	if updated.Status.StateCode != int32(types.StateRunning) {
		t.Errorf("status.stateCode = %d, want %d", updated.Status.StateCode, int32(types.StateRunning))
	}
}

// TestDrift_NoDrift verifies that a CRD whose status already matches the tracker
// state results in no patch calls.
func TestDrift_NoDrift(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000008"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-no-drift")

	crd := liveCRD(domain, "uid-no-drift")
	c := buildClient(crd)
	// Seed the status matching the tracker state exactly.
	expected := v1alpha1.DomainToLibvirtDomain(domain)
	crd.Status = expected.Status
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	patchCalled := false
	c = buildClientWithInterceptor(interceptor.Funcs{
		SubResourcePatch: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
			patchCalled = true
			return nil
		},
	}, crd)
	// Re-seed the status on the interceptor-wrapped client.
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("re-seed status on interceptor client: %v", err)
	}

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{uuid: domain}}
	r := newDriftCtrl(c, tracker, newFakeInvalidator())

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if patchCalled {
		t.Error("Status().Patch should not be called when there is no drift")
	}
}

// TestDrift_UnknownTracked_SetsGone verifies that when a CRD exists but the
// tracker has no entry for it (domain vanished), the drift controller marks it
// as gone with PotentiallyCorrupted and a deletedAt timestamp.
func TestDrift_UnknownTracked_SetsGone(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-000000000009"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-unknown")

	crd := liveCRD(domain, "uid-unknown")
	c := buildClient(crd)
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	// Tracker has no entry for this UUID.
	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{}}
	r := newDriftCtrl(c, tracker, newFakeInvalidator())

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.State != types.StateGone.String() {
		t.Errorf("status.state = %q, want gone", updated.Status.State)
	}
	if updated.Status.DeletedAt == "" {
		t.Error("status.deletedAt should be set")
	}
	if !updated.Status.PotentiallyCorrupted {
		t.Error("status.potentiallyCorrupted should be true")
	}
}

// TestDrift_UnknownTracked_Idempotent verifies that a second reconcile on a CRD
// already marked gone/corrupted does not re-patch (needsPatch stays false).
func TestDrift_UnknownTracked_Idempotent(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-00000000000a"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-idempotent")

	crd := liveCRD(domain, "uid-idempotent")
	// Pre-seed the CRD already in gone/corrupted state.
	crd.Status.State = types.StateGone.String()
	crd.Status.StateCode = int32(types.StateGone)
	crd.Status.DeletedAt = time.Now().UTC().Format(metav1.RFC3339Micro)
	crd.Status.PotentiallyCorrupted = true

	patchCalled := false
	c := buildClientWithInterceptor(interceptor.Funcs{
		SubResourcePatch: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
			patchCalled = true
			return nil
		},
	}, crd)
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("seed gone status: %v", err)
	}

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{}}
	r := newDriftCtrl(c, tracker, newFakeInvalidator())

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if patchCalled {
		t.Error("Status().Patch should not be called when CRD is already fully marked gone/corrupted")
	}
}

// TestDrift_UnknownTracked_RepairsInvalidDeletedAt verifies that malformed
// retention timestamps are replaced so the retention controller can eventually
// delete the corrupted CRD.
func TestDrift_UnknownTracked_RepairsInvalidDeletedAt(t *testing.T) {
	uuid := "aaaabbbb-cccc-dddd-eeee-00000000000d"
	crdName := "test-host-" + uuid
	domain := domainFixture(uuid, "vm-invalid-deleted-at")

	crd := liveCRD(domain, "uid-invalid-deleted-at")
	crd.Status.State = types.StateGone.String()
	crd.Status.StateCode = int32(types.StateGone)
	crd.Status.DeletedAt = "not-a-timestamp"
	crd.Status.PotentiallyCorrupted = true

	c := buildClient(crd)
	if err := c.Status().Update(context.Background(), crd); err != nil {
		t.Fatalf("seed invalid deletedAt status: %v", err)
	}

	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{}}
	r := newDriftCtrl(c, tracker, newFakeInvalidator())

	_, err := r.Reconcile(context.Background(), driftReq(crdName))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: crdName}, updated); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status.DeletedAt == "" || updated.Status.DeletedAt == "not-a-timestamp" {
		t.Fatalf("status.deletedAt was not repaired: %q", updated.Status.DeletedAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, updated.Status.DeletedAt); err != nil {
		t.Fatalf("repaired deletedAt is not parseable: %q: %v", updated.Status.DeletedAt, err)
	}
}

// TestDrift_ShortCRDName verifies that a CRD name too short to contain a UUID
// is silently skipped without error.
func TestDrift_ShortCRDName(t *testing.T) {
	tracker := &fakeTracker{ready: true, domains: map[string]*types.DomainState{}}
	r := newDriftCtrl(buildClient(), tracker, newFakeInvalidator())

	result, err := r.Reconcile(context.Background(), driftReq("too-short"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result for unparsable CRD name, got %+v", result)
	}
}

// -- statusEqual tests --------------------------------------------------------

func TestStatusEqual_SameValues(t *testing.T) {
	domain := domainFixture("uuid-se-1", "vm-se")
	crd := v1alpha1.DomainToLibvirtDomain(domain)
	if !statusEqual(crd.Status, crd.Status) {
		t.Error("identical statuses should be equal")
	}
}

func TestStatusEqual_StateDiffers(t *testing.T) {
	domain := domainFixture("uuid-se-2", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	b := a
	b.State = "shutoff"
	b.StateCode = int32(types.StateShutoff)
	if statusEqual(a, b) {
		t.Error("statuses with different State should not be equal")
	}
}

func TestStatusEqual_XMLHashDiffers(t *testing.T) {
	domain := domainFixture("uuid-se-3", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	b := a
	b.XMLHash = "different-hash"
	if statusEqual(a, b) {
		t.Error("statuses with different XMLHash should not be equal")
	}
}

func TestStatusEqual_MemoryDiffers(t *testing.T) {
	domain := domainFixture("uuid-se-4", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	b := a
	b.MemoryKB = a.MemoryKB + 1024
	if statusEqual(a, b) {
		t.Error("statuses with different MemoryKB should not be equal")
	}
}

func TestStatusEqual_PotentiallyCorruptedDiffers(t *testing.T) {
	domain := domainFixture("uuid-se-5", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	b := a
	b.PotentiallyCorrupted = true
	if statusEqual(a, b) {
		t.Error("statuses with different PotentiallyCorrupted should not be equal")
	}
}

func TestStatusEqual_DomainXMLNilVsNil(t *testing.T) {
	domain := domainFixture("uuid-se-6", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	// domainFixture has no XML, so DomainXML should be nil on both sides.
	if !statusEqual(a, a) {
		t.Error("both-nil DomainXML should be equal")
	}
}

func TestStatusEqual_LastChangedIgnored(t *testing.T) {
	domain := domainFixture("uuid-se-7", "vm-se")
	a := v1alpha1.DomainToLibvirtDomain(domain).Status
	b := a
	// LastChanged is intentionally excluded from statusEqual — it changes every
	// write and is operator-managed, not tracker-managed.
	b.LastChanged = "2099-01-01T00:00:00.000000Z"
	if !statusEqual(a, b) {
		t.Error("LastChanged difference should not cause statusEqual to return false")
	}
}
