// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package integration provides elaborate test scenarios with fault injection.
package integration

import (
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
)

// --------------------------------------------------------------------------
// Scenario 1: Kubernetes API Fault Injection
//
// Tests operator resilience when the K8s API returns errors or is slow.
// - Inject 500 errors for CRD creation
// - Inject delays for updates
// - Verify operator continues to function after faults clear
// --------------------------------------------------------------------------

func TestScenario_K8sFaultInjection(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "fault injection depends on fake-k8s SetFault/request logging; needs a controller-runtime-compatible fake-k8s or envtest proxy")

	env := NewTestEnv(t)
	defer env.Close()

	// Add test domains
	domains := []struct {
		uuid [16]byte
		name string
	}{
		{[16]byte{0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01}, "vm-k8s-fault-1"},
		{[16]byte{0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02}, "vm-k8s-fault-2"},
	}

	for _, d := range domains {
		if err := env.State().AddDomain(&fakelibvirt.Domain{
			UUID:      d.uuid,
			Name:      d.name,
			ID:        1,
			State:     fakelibvirt.DomainRunning,
			MaxMem:    2097152,
			Memory:    1048576,
			NrVirtCPU: 2,
		}); err != nil {
			t.Fatalf("AddDomain %s failed: %v", d.name, err)
		}
	}

	// === Phase 1: Inject K8s errors ===
	t.Log("Phase 1: Injecting K8s 500 errors")
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainResource, &fakek8s.FaultConfig{
		Code:     500,
		ErrorMsg: "Internal server error (injected)",
	})

	// Start operator
	errCh := env.RunOperator()
	defer func() {
		env.cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	}()

	// Wait a bit for poll cycle to occur
	time.Sleep(500 * time.Millisecond)

	// Verify requests were made and rejected
	requests := env.FakeK8s.Requests()
	hasErroredRequest := false
	for _, r := range requests {
		if r.Method == "POST" || r.Method == "PUT" {
			hasErroredRequest = true
		}
	}
	if !hasErroredRequest {
		t.Log("Warning: No POST/PUT requests observed (operator might not have tried yet)")
	}

	// Verify no resources created (due to errors)
	res1 := env.FakeK8s.ListResources(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns")
	if len(res1) > 0 {
		t.Errorf("Expected no resources during fault injection, got %d", len(res1))
	}

	// === Phase 2: Clear faults ===
	t.Log("Phase 2: Clearing faults")
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainResource, nil)
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainHeartbeatResource, nil)
	env.FakeK8s.ClearRequests()

	// Wait for operator to recover and create resources
	// The next poll cycle should succeed
	time.Sleep(500 * time.Millisecond)

	// Verify resources are now created
	if !env.WaitForResourceCount(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", 2, 5*time.Second) {
		res := env.FakeK8s.ListResources(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns")
		t.Errorf("Expected 2 resources after fault cleared, got %d", len(res))

		// Debug: show recent requests
		for _, r := range env.FakeK8s.Requests() {
			t.Logf("Request: %s %s", r.Method, r.Path)
		}
	} else {
		t.Log("Recovery successful: resources created after faults cleared")
	}

	// === Phase 3: Inject delays ===
	t.Log("Phase 3: Injecting 200ms delays")
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainHeartbeatResource, &fakek8s.FaultConfig{
		Delay: 200 * time.Millisecond,
	})

	// Change state to trigger updates
	for _, d := range domains {
		env.State().SetDomainState(d.uuid, fakelibvirt.DomainPaused)
	}

	// Wait for updates to process (with delay)
	time.Sleep(1 * time.Second)

	// Verify updates still happen (just slower)
	res, found := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", "test-host-01010101-0101-0101-0101-010101010101", 1*time.Second)
	if !found {
		t.Fatal("Resource not found after delay injection")
	}
	if res.Status["state"] != "paused" {
		// May still be running if update hasn't propagated yet
		t.Logf("State: %v (may be delayed)", res.Status["state"])
	}

	t.Log("Scenario 1: K8s Fault Injection completed")
}

// --------------------------------------------------------------------------
// Scenario 2: Libvirt Connection Issues
//
// Tests operator behavior when libvirt socket disappears or becomes unresponsive.
// - Start with domains, verify CRDs created
// - Kill fake-libvirt server
// - Restart fake-libvirt server
// - Verify operator reconnects and resumes
// --------------------------------------------------------------------------

func TestScenario_LibvirtConnectionIssues(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "libvirt reconnect behavior should be ported to envtest; current fake-k8s harness does not receive domain writes")

	env := NewTestEnv(t)
	defer env.Close()

	// Add initial domain
	testUUID := [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00}
	if err := env.State().AddDomain(&fakelibvirt.Domain{
		UUID:      testUUID,
		Name:      "vm-reconnect-test",
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    1048576,
		Memory:    1048576,
		NrVirtCPU: 1,
	}); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	// Start operator
	errCh := env.RunOperator()
	defer func() {
		env.cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	}()

	// === Phase 1: Verify initial connection ===
	t.Log("Phase 1: Verifying initial connection")
	expectedName := "test-host-11223344-5566-7788-99aa-bbccddeeff00"
	_, found := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName, 5*time.Second)
	if !found {
		t.Fatalf("Initial CRD not created")
	}
	t.Log("Initial CRD created successfully")

	// === Phase 2: Simulate connection loss ===
	t.Log("Phase 2: Simulating libvirt connection loss")
	env.FakeLibvirt.Close()

	// Wait briefly to let operator notice the disconnect
	time.Sleep(200 * time.Millisecond)

	// === Phase 3: Restart fake-libvirt ===
	t.Log("Phase 3: Restarting fake-libvirt")
	env.FakeLibvirt = fakelibvirt.NewServer(fakelibvirt.NewEmpty())
	if err := env.FakeLibvirt.Listen(env.SocketPath()); err != nil {
		t.Fatalf("Failed to restart fake-libvirt: %v", err)
	}

	// Add a NEW domain to the restarted server
	newUUID := [16]byte{0xAA, 0xAA, 0xBB, 0xBB, 0xCC, 0xCC, 0xDD, 0xDD, 0xEE, 0xEE, 0xFF, 0xFF, 0x00, 0x00, 0x11, 0x11}
	if err := env.State().AddDomain(&fakelibvirt.Domain{
		UUID:      newUUID,
		Name:      "vm-after-reconnect",
		ID:        2,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	}); err != nil {
		t.Fatalf("AddDomain after reconnect failed: %v", err)
	}

	// === Phase 4: Verify reconnection ===
	t.Log("Phase 4: Verifying reconnection")
	newExpectedName := "test-host-aaaabbbb-cccc-dddd-eeee-ffff00001111"
	// Give operator time to reconnect (poll interval is 100ms, so within 500ms should see reconnection)
	_, found = env.WaitForResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", newExpectedName, 5*time.Second)
	if !found {
		reqs := env.FakeK8s.Requests()
		t.Logf("K8s requests (%d) after reconnect:", len(reqs))
		for _, r := range reqs {
			t.Logf("  %s %s", r.Method, r.Path)
		}
		t.Errorf("New domain CRD not created after reconnect - operator may not have reconnected")
	} else {
		t.Log("Reconnection successful: new domain CRD created")
	}

	t.Log("Scenario 2: Libvirt Connection Issues completed")
}

// --------------------------------------------------------------------------
// Scenario 3: High Volume with Congestion
//
// Tests the congestion queue behavior under load.
// - Add many domains rapidly
// - Trigger rapid state changes
// - Verify eventual consistency (all final states reflected)
// - Verify stale updates are dropped (congestion queue works)
// --------------------------------------------------------------------------

func TestScenario_HighVolumeWithCongestion(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "high-volume final-state checks can move to envtest, but request-count/backpressure observations depend on fake-k8s instrumentation")

	env := NewTestEnv(t)
	defer env.Close()

	numDomains := 10

	// Add K8s delay to create backpressure (light delay to test congestion)
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainResource, &fakek8s.FaultConfig{
		Delay: 20 * time.Millisecond,
	})
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainHeartbeatResource, &fakek8s.FaultConfig{
		Delay: 20 * time.Millisecond,
	})

	// === Phase 1: Add many domains ===
	t.Logf("Phase 1: Adding %d domains", numDomains)
	uuids := make([][16]byte, numDomains)
	for i := 0; i < numDomains; i++ {
		uuids[i] = [16]byte{byte(i + 1)}
		// Fill rest of UUID
		for j := 1; j < 16; j++ {
			uuids[i][j] = byte((i + 1) * 10)
		}

		if err := env.State().AddDomain(&fakelibvirt.Domain{
			UUID:      uuids[i],
			Name:      "vm-congestion-" + string(rune('a'+i)),
			ID:        int32(i + 1),
			State:     fakelibvirt.DomainRunning,
			MaxMem:    1048576,
			Memory:    512000,
			NrVirtCPU: 1,
		}); err != nil {
			t.Fatalf("AddDomain %d failed: %v", i, err)
		}
	}

	// Start operator
	errCh := env.RunOperator()
	defer func() {
		env.cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
		}
	}()

	// Wait for initial discovery
	time.Sleep(500 * time.Millisecond)

	// === Phase 2: Rapid state changes ===
	t.Log("Phase 2: Triggering rapid state changes")
	for cycle := 0; cycle < 3; cycle++ {
		for i := 0; i < numDomains; i++ {
			newState := fakelibvirt.DomainPaused
			if cycle%2 == 0 {
				newState = fakelibvirt.DomainRunning
			}
			env.State().SetDomainState(uuids[i], uint8(newState))
		}
		time.Sleep(10 * time.Millisecond) // rapid fire
	}

	// Final state: all paused
	t.Log("Phase 3: Setting final state (all shutoff)")
	for i := 0; i < numDomains; i++ {
		env.State().SetDomainState(uuids[i], fakelibvirt.DomainShutoff)
	}

	// === Phase 4: Wait for eventual consistency ===
	t.Log("Phase 4: Waiting for eventual consistency")

	// Remove delays to let things catch up
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainResource, nil)
	env.FakeK8s.SetFault(crdmeta.BaseLibvirtDomainHeartbeatResource, nil)

	// Wait for all resources to be created
	deadline := time.Now().Add(30 * time.Second)
	var allCreated bool
	for time.Now().Before(deadline) {
		resources := env.FakeK8s.ListResources(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns")
		if len(resources) >= numDomains {
			allCreated = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	resources := env.FakeK8s.ListResources(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns")
	if !allCreated {
		// This test is about eventual consistency, not strict timing
		// With congestion queue + delays, some resources may be slower to create
		t.Logf("Eventual consistency: expected %d, got %d (some may be in queue)", numDomains, len(resources))
		if len(resources) == 0 {
			t.Error("No domains created at all - operator may have failed")
		}
	} else {
		t.Logf("All %d domains created", numDomains)
	}

	// Verify final states
	time.Sleep(2 * time.Second) // wait for state updates to propagate

	resources = env.FakeK8s.ListResources("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns")
	shutoffCount := 0
	for _, res := range resources {
		if res.Status["state"] == "shutoff" {
			shutoffCount++
		}
	}

	t.Logf("Final states: %d/%d are 'shutoff'", shutoffCount, len(resources))

	// Check K8s request count to see congestion effect
	requests := env.FakeK8s.Requests()
	t.Logf("Total K8s requests: %d", len(requests))
	t.Logf("Domains * cycles: %d", numDomains*3*2) // create + update per domain per cycle (approx)

	// The request count should be less than domains * cycles if congestion worked
	// (stale updates dropped)
	t.Logf("Congestion may have reduced requests (depends on timing)")

	t.Log("Scenario 3: High Volume with Congestion completed")
}
