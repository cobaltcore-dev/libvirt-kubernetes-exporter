// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

// testScheme is shared across all tests in this package.
var testScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(s))
	metav1.AddToGroupVersion(s, v1alpha1.GroupVersion)
	return s
}()

// -- fixtures -----------------------------------------------------------------

// domainFixture returns a live (not gone) DomainState.
func domainFixture(uuid, name string) *types.DomainState {
	d := &types.DomainState{
		UUID:      uuid,
		Name:      name,
		Host:      "test-host",
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
		FirstSeen: time.Now().Add(-10 * time.Second),
	}
	d.Status.Update(types.StateRunning, time.Now(), time.Now(), types.SourcePoll)
	return d
}

// goneDomain returns a DomainState with DeletedAt set.
func goneDomain(uuid, name string) *types.DomainState {
	d := domainFixture(uuid, name)
	t := time.Now()
	d.DeletedAt = &t
	d.Status.Update(types.StateGone, time.Now(), time.Now(), types.SourcePoll)
	return d
}

// -- fakeKnownStore -----------------------------------------------------------

// fakeKnownStore records SetKnownDomain / InvalidateDomain calls for assertions.
type fakeKnownStore struct {
	known       map[string]string
	invalidated []string
}

func newFakeKnownStore() *fakeKnownStore {
	return &fakeKnownStore{known: make(map[string]string)}
}

func (f *fakeKnownStore) SetKnownDomain(uuid, uid string) { f.known[uuid] = uid }
func (f *fakeKnownStore) InvalidateDomain(uuid string)    { f.invalidated = append(f.invalidated, uuid) }

// -- helpers ------------------------------------------------------------------

// buildClient creates a fake client with the test scheme and status subresource
// support for LibvirtDomain (mirrors real K8s: status is a separate subresource).
func buildClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.LibvirtDomain{}).
		Build()
}

// buildClientWithInterceptor creates a fake client that intercepts specific
// calls, used to inject transient K8s API failures.
func buildClientWithInterceptor(fns interceptor.Funcs, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.LibvirtDomain{}).
		WithInterceptorFuncs(fns).
		Build()
}

// crdWithUID returns a LibvirtDomain CRD pre-seeded with a UID so that the
// fake client (which does not auto-assign UIDs) returns it through CreateOrPatch.
func crdWithUID(domain *types.DomainState, uid string) *v1alpha1.LibvirtDomain {
	crd := v1alpha1.DomainToLibvirtDomain(domain)
	crd.UID = k8stypes.UID(uid)
	return crd
}

// newCtrl builds a DomainWriteController with an initialised pending map.
func newCtrl(c client.Client, store *fakeKnownStore) *DomainWriteController {
	return &DomainWriteController{
		Client:     c,
		KnownStore: store,
		pending:    make(map[string]*types.Update),
	}
}

func reconcileReq(uuid string) reconcile.Request {
	return reconcile.Request{NamespacedName: k8stypes.NamespacedName{Name: uuid}}
}

func makeUpdate(d *types.DomainState, ut types.UpdateType) *types.Update {
	return &types.Update{Domain: d, Type: ut, Timestamp: time.Now()}
}

// setPending inserts an update directly into the controller's pending map,
// simulating what the GenericFunc handler does when an event arrives.
func setPending(r *DomainWriteController, upd *types.Update) {
	r.mu.Lock()
	r.pending[upd.Domain.UUID] = upd
	r.mu.Unlock()
}

func getCRD(t *testing.T, c client.Client, name string) *v1alpha1.LibvirtDomain {
	t.Helper()
	crd := &v1alpha1.LibvirtDomain{}
	if err := c.Get(context.Background(), k8stypes.NamespacedName{Name: name}, crd); err != nil {
		t.Fatalf("Get CRD %q: %v", name, err)
	}
	return crd
}

// -- tests --------------------------------------------------------------------

// TestDomainWrite_Create verifies that a new domain creates a CRD with the
// correct status fields and registers the UUID in KnownStore.
func TestDomainWrite_Create(t *testing.T) {
	store := newFakeKnownStore()
	domain := domainFixture("aaaabbbb-cccc-dddd-eeee-111111111111", "vm-create")
	// Pre-seed the CRD with a UID so the fake client returns it after CreateOrPatch.
	// A real API server always assigns a UID; the fake client only preserves what is set.
	existing := crdWithUID(domain, "test-uid-aaaa")
	c := buildClient(existing)

	r := newCtrl(c, store)
	setPending(r, makeUpdate(domain, types.UpdateCreated))

	if _, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-111111111111")); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	crd := getCRD(t, c, "test-host-aaaabbbb-cccc-dddd-eeee-111111111111")
	if crd.Status.UUID != "aaaabbbb-cccc-dddd-eeee-111111111111" {
		t.Errorf("status.uuid = %q, want aaaabbbb-cccc-dddd-eeee-111111111111", crd.Status.UUID)
	}
	if crd.Status.State != "running" {
		t.Errorf("status.state = %q, want running", crd.Status.State)
	}
	if crd.Status.Name != "vm-create" {
		t.Errorf("status.name = %q, want vm-create", crd.Status.Name)
	}
	if uid, ok := store.known["aaaabbbb-cccc-dddd-eeee-111111111111"]; !ok || uid != "test-uid-aaaa" {
		t.Errorf("SetKnownDomain not called with correct UID after create; known = %v", store.known)
	}
}

// TestDomainWrite_Update verifies that writing over an existing CRD updates
// its status to the latest observed state.
func TestDomainWrite_Update(t *testing.T) {
	store := newFakeKnownStore()
	domain := domainFixture("aaaabbbb-cccc-dddd-eeee-222222222222", "vm-update")
	existing := crdWithUID(domain, "test-uid-bbbb")
	c := buildClient(existing)
	if err := c.Status().Update(context.Background(), existing); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	r := newCtrl(c, store)
	domain.Status.Update(types.StateShutoff, time.Now(), time.Now(), types.SourcePoll)
	setPending(r, makeUpdate(domain, types.UpdateDataChanged))

	if _, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-222222222222")); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	crd := getCRD(t, c, "test-host-aaaabbbb-cccc-dddd-eeee-222222222222")
	if crd.Status.State != "shutoff" {
		t.Errorf("status.state = %q, want shutoff", crd.Status.State)
	}
	if uid, ok := store.known["aaaabbbb-cccc-dddd-eeee-222222222222"]; !ok || uid != "test-uid-bbbb" {
		t.Errorf("SetKnownDomain not called with correct UID after update; known = %v", store.known)
	}
}

// TestDomainWrite_NoPending verifies that a reconcile request for a UUID with
// no queued update is a silent no-op.
func TestDomainWrite_NoPending(t *testing.T) {
	store := newFakeKnownStore()
	r := newCtrl(buildClient(), store)

	result, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-333333333333"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected empty Result for no-op, got %+v", result)
	}
	if len(store.known) > 0 || len(store.invalidated) > 0 {
		t.Error("KnownStore should not be touched for a no-op reconcile")
	}
}

// TestDomainWrite_GoneMarksDeleted verifies that a gone domain patches the
// existing CRD status with deletedAt + stateGone and invalidates the store.
func TestDomainWrite_GoneMarksDeleted(t *testing.T) {
	store := newFakeKnownStore()
	existing := v1alpha1.DomainToLibvirtDomain(domainFixture("aaaabbbb-cccc-dddd-eeee-444444444444", "vm-gone"))
	c := buildClient(existing)
	if err := c.Status().Update(context.Background(), existing); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	r := newCtrl(c, store)
	setPending(r, makeUpdate(goneDomain("aaaabbbb-cccc-dddd-eeee-444444444444", "vm-gone"), types.UpdateDataChanged))

	if _, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-444444444444")); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	crd := getCRD(t, c, "test-host-aaaabbbb-cccc-dddd-eeee-444444444444")
	if crd.Status.State != types.StateGone.String() {
		t.Errorf("status.state = %q, want %q", crd.Status.State, types.StateGone.String())
	}
	if crd.Status.DeletedAt == "" {
		t.Error("status.deletedAt should be set on a gone domain")
	}
	if len(store.invalidated) == 0 || store.invalidated[0] != "aaaabbbb-cccc-dddd-eeee-444444444444" {
		t.Error("InvalidateDomain not called after marking gone")
	}
}

// TestDomainWrite_GoneNotFound verifies that marking a domain gone when the
// CRD is already absent is a silent no-op.
func TestDomainWrite_GoneNotFound(t *testing.T) {
	store := newFakeKnownStore()
	r := newCtrl(buildClient(), store) // empty store — CRD doesn't exist
	setPending(r, makeUpdate(goneDomain("aaaabbbb-cccc-dddd-eeee-555555555555", "vm-gone-nf"), types.UpdateDataChanged))

	if _, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-555555555555")); err != nil {
		t.Fatalf("Reconcile should not error on NotFound for gone domain: %v", err)
	}
}

// TestDomainWrite_StaleDrop verifies that when two updates are queued for the
// same UUID before reconcile runs, only the latest one is written.
func TestDomainWrite_StaleDrop(t *testing.T) {
	store := newFakeKnownStore()
	r := newCtrl(buildClient(), store)

	old := domainFixture("aaaabbbb-cccc-dddd-eeee-666666666666", "vm-stale")
	old.Status.Update(types.StateRunning, time.Now(), time.Now(), types.SourcePoll)

	latest := domainFixture("aaaabbbb-cccc-dddd-eeee-666666666666", "vm-stale")
	latest.Status.Update(types.StateShutoff, time.Now(), time.Now(), types.SourcePoll)

	// Simulate the GenericFunc handler: second write overwrites the first.
	r.mu.Lock()
	r.pending["aaaabbbb-cccc-dddd-eeee-666666666666"] = makeUpdate(old, types.UpdateCreated)
	r.pending["aaaabbbb-cccc-dddd-eeee-666666666666"] = makeUpdate(latest, types.UpdateDataChanged)
	r.mu.Unlock()

	if _, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-666666666666")); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	crd := getCRD(t, r.Client, "test-host-aaaabbbb-cccc-dddd-eeee-666666666666")
	if crd.Status.State != "shutoff" {
		t.Errorf("status.state = %q, want shutoff (stale running update should have been dropped)", crd.Status.State)
	}
}

// TestDomainWrite_ErrorRequeues verifies that when the K8s status patch fails,
// the update is put back into pending so the next reconcile will retry it.
func TestDomainWrite_ErrorRequeues(t *testing.T) {
	store := newFakeKnownStore()
	patchCalls := 0
	c := buildClientWithInterceptor(interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patchCalls++
			return k8serrors.NewServiceUnavailable("injected failure")
		},
	})
	r := newCtrl(c, store)

	setPending(r, makeUpdate(domainFixture("aaaabbbb-cccc-dddd-eeee-777777777777", "vm-err"), types.UpdateCreated))

	_, err := r.Reconcile(context.Background(), reconcileReq("aaaabbbb-cccc-dddd-eeee-777777777777"))
	if err == nil {
		t.Fatal("expected error to propagate from failed K8s write")
	}

	r.mu.Lock()
	_, stillPending := r.pending["aaaabbbb-cccc-dddd-eeee-777777777777"]
	r.mu.Unlock()

	if !stillPending {
		t.Error("update should have been put back into pending after a write error")
	}
	if patchCalls == 0 {
		t.Error("expected at least one status patch attempt before failure")
	}
}
