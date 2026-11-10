// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package integration provides end-to-end tests for the operator.
//
// These tests run the operator against fake-libvirt and fake-k8s HTTP servers,
// demonstrating full mock-based testing without stepping.
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/operator"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/tracing"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// TestEnv holds the test environment.
type TestEnv struct {
	FakeLibvirt *fakelibvirt.Server
	FakeK8s     *fakek8s.Server
	Operator    *operator.Operator

	tmpDir     string
	socketPath string // exported via SocketPath()
	ctx        context.Context
	cancel     context.CancelFunc

	// Tracing
	tracer      *tracing.Collector
	traceWriter *tracing.Writer
}

// SocketPath returns the path to the libvirt socket.
func (e *TestEnv) SocketPath() string {
	return e.socketPath
}

func NewTestEnv(t *testing.T) *TestEnv {
	return NewTestEnvWithName(t, t.Name())
}

func skipLegacyFakeK8sHarness(t *testing.T, reason string) {
	t.Helper()
	t.Skipf("legacy fake-k8s operator harness disabled during controller-runtime migration: %s. The harness uses operator.NewWithExporter, which does not currently wire the manager/updateEventCh/DomainWriteController path; preserve this test for future fake-k8s fault-injection work", reason)
}

func NewTestEnvWithName(t *testing.T, testName string) *TestEnv {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "integration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	env := &TestEnv{
		FakeLibvirt: fakelibvirt.NewServer(fakelibvirt.NewEmpty()),
		FakeK8s:     fakek8s.New(),
		tmpDir:      tmpDir,
		socketPath:  filepath.Join(tmpDir, "libvirt.sock"),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Set up tracing - write to a shared traces directory
	tracesDir := os.Getenv("TRACE_OUTPUT_DIR")
	if tracesDir == "" {
		tracesDir = filepath.Join(tmpDir, "traces")
	}
	env.tracer = tracing.NewCollector(1)
	env.traceWriter, err = tracing.NewWriter(tracesDir, testName)
	if err != nil {
		t.Logf("Warning: failed to create trace writer: %v", err)
	} else {
		env.tracer.SetWriter(env.traceWriter)
		env.tracer.SetEnabled(true)
		t.Logf("Tracing enabled, writing to: %s", env.traceWriter.Path())
	}

	if err := env.FakeLibvirt.Listen(env.socketPath); err != nil {
		env.Close()
		t.Fatalf("Failed to start fake-libvirt: %v", err)
	}

	// Create dynamic client pointing at fake k8s
	k8sConfig := &rest.Config{
		Host: env.FakeK8s.URL(),
	}
	dynClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		env.Close()
		t.Fatalf("Failed to create k8s client: %v", err)
	}

	cfg := &config.Config{
		LibvirtSocket:        env.socketPath,
		LibvirtURI:           "qemu:///system",
		PollInterval:         100 * time.Millisecond, // fast polling for tests
		Hostname:             "test-host",
		ObservationQueueSize: 100,
		UpdateQueueSize:      100,
	}

	env.Operator, err = operator.NewWithExporter(cfg, dynClient)
	if err != nil {
		env.Close()
		t.Fatalf("Failed to create operator: %v", err)
	}

	// Wire up tracing to operator components
	if env.tracer != nil {
		traceCallback := func(cat, name, phase string, args map[string]any) {
			if phase == "i" {
				env.tracer.Instant(name, cat, args)
			} else if phase == "B" {
				env.tracer.Begin(name, cat)
			} else if phase == "E" {
				env.tracer.End(name, cat)
			}
		}
		env.Operator.Tracker().SetTraceCallback(traceCallback)
		env.Operator.Exporter().SetTraceCallback(traceCallback)
	}

	return env
}

func (e *TestEnv) Close() {
	e.cancel()
	if e.traceWriter != nil {
		e.traceWriter.Close()
	}
	if e.FakeLibvirt != nil {
		e.FakeLibvirt.Close()
	}
	if e.FakeK8s != nil {
		e.FakeK8s.Close()
	}
	// Only remove tmpDir if TRACE_OUTPUT_DIR is set (traces are written elsewhere)
	if e.tmpDir != "" && os.Getenv("TRACE_OUTPUT_DIR") != "" {
		os.RemoveAll(e.tmpDir)
	} else if e.tmpDir != "" {
		// Keep tmpDir with traces, but log its location
		// Tests can check TRACE_OUTPUT_DIR or tmpDir/traces for trace files
	}
}

// TracePath returns the path to the trace file (if tracing is enabled).
func (e *TestEnv) TracePath() string {
	if e.traceWriter != nil {
		return e.traceWriter.Path()
	}
	return ""
}

func (e *TestEnv) State() *fakelibvirt.FakeState {
	return e.FakeLibvirt.State()
}

// RunOperator starts the operator in the background.
func (e *TestEnv) RunOperator() chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- e.Operator.Run(e.ctx)
	}()
	return errCh
}

// WaitForResource polls for a resource to appear in fake-k8s.
func (e *TestEnv) WaitForResource(group, version, resource, namespace, name string, timeout time.Duration) (*fakek8s.Resource, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if res, ok := e.FakeK8s.GetResource(group, version, resource, namespace, name); ok {
			return res, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, false
}

// WaitForResourceCount polls until expected number of resources exist.
func (e *TestEnv) WaitForResourceCount(group, version, resource, namespace string, count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resources := e.FakeK8s.ListResources(group, version, resource, namespace)
		if len(resources) == count {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// --------------------------------------------------------------------------
// Basic Integration Tests
// --------------------------------------------------------------------------

// TestBasicDomainDiscovery tests that the operator discovers domains from libvirt
// and creates CRDs in Kubernetes.
func TestBasicDomainDiscovery(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "basic discovery is now covered by TestEnvtest_DomainAppears")

	env := NewTestEnv(t)
	defer env.Close()

	// Add a domain to fake-libvirt
	testDomain := &fakelibvirt.Domain{
		UUID:      [16]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0},
		Name:      "test-vm-discover",
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	}
	if err := env.State().AddDomain(testDomain); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	// Start operator
	errCh := env.RunOperator()

	// Wait for the domain CRD to appear
	expectedName := "test-host-12345678-9abc-def0-1234-56789abcdef0"
	res, found := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName, 5*time.Second)
	if !found {
		reqs := env.FakeK8s.Requests()
		t.Logf("K8s requests received (%d):", len(reqs))
		for _, r := range reqs {
			t.Logf("  %s %s", r.Method, r.Path)
		}
		t.Fatalf("LibvirtDomain CRD not created within timeout")
	}

	// Verify resource content
	if res.Status["state"] != "running" {
		t.Errorf("expected state 'running', got %v", res.Status["state"])
	}

	// Also check heartbeat CRD
	hbRes, hbFound := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainHeartbeatResource(), "test-ns", expectedName, 5*time.Second)
	if !hbFound {
		t.Fatalf("LibvirtDomainHeartbeat CRD not created")
	}
	if hbRes.Status["ttl"] != "100ms" {
		t.Errorf("expected ttl '100ms', got %v", hbRes.Status["ttl"])
	}

	t.Logf("Domain discovered and CRDs created successfully")

	// Shutdown
	env.cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("Operator error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Operator didn't shut down")
	}
}

// TestStateChange tests that state changes in libvirt are reflected in CRDs.
func TestStateChange(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "state propagation is now covered by TestEnvtest_StateChange")

	env := NewTestEnv(t)
	defer env.Close()

	// Add a running domain
	testUUID := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00}
	testDomain := &fakelibvirt.Domain{
		UUID:      testUUID,
		Name:      "test-vm-state",
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	}
	if err := env.State().AddDomain(testDomain); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	// Start operator
	errCh := env.RunOperator()
	defer env.cancel()

	// Wait for initial CRD
	expectedName := "test-host-aabbccdd-eeff-1122-3344-556677889900"
	res, found := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName, 5*time.Second)
	if !found {
		t.Fatalf("Initial CRD not created")
	}
	if res.Status["state"] != "running" {
		t.Errorf("initial state should be 'running', got %v", res.Status["state"])
	}

	// Change state to shutoff
	if err := env.State().SetDomainState(testUUID, fakelibvirt.DomainShutoff); err != nil {
		t.Fatalf("SetDomainState failed: %v", err)
	}

	// Wait for CRD to update (poll will pick up the change)
	deadline := time.Now().Add(5 * time.Second)
	var updated bool
	for time.Now().Before(deadline) {
		res, _ = env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName, 100*time.Millisecond)
		if res != nil && res.Status["state"] == "shutoff" {
			updated = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !updated {
		t.Errorf("CRD state was not updated to 'shutoff' within timeout")
	}

	t.Logf("State change reflected in CRD")

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}

// TestDomainDeletion tests that domain removal is reflected in CRDs.
func TestDomainDeletion(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "delete semantics now require controller-runtime retention and are covered by envtest deletion/retention tests")

	env := NewTestEnv(t)
	defer env.Close()

	// Add a domain
	testUUID := [16]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB}
	testDomain := &fakelibvirt.Domain{
		UUID:      testUUID,
		Name:      "test-vm-delete",
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    1048576,
		Memory:    1048576,
		NrVirtCPU: 1,
	}
	if err := env.State().AddDomain(testDomain); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	// Start operator
	errCh := env.RunOperator()
	defer env.cancel()

	// Wait for CRD creation
	expectedName := "test-host-deadbeef-0011-2233-4455-66778899aabb"
	_, found := env.WaitForResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", expectedName, 5*time.Second)
	if !found {
		t.Fatalf("CRD not created")
	}

	// Remove domain from libvirt
	if err := env.State().RemoveDomain(testUUID); err != nil {
		t.Fatalf("RemoveDomain failed: %v", err)
	}

	// Wait for CRD to be deleted
	deadline := time.Now().Add(5 * time.Second)
	var deleted bool
	for time.Now().Before(deadline) {
		_, exists := env.FakeK8s.GetResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName)
		if !exists {
			deleted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !deleted {
		t.Errorf("CRD was not deleted within timeout")
	}

	t.Logf("Domain deletion reflected in CRDs")

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}
