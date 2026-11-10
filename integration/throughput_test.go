// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package integration provides throughput measurement tests.
package integration

import (
	"fmt"
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
)

// TestThroughput_StateToggle measures the maximum rate at which state changes
// can propagate through the operator to fake-k8s.
//
// It toggles a domain between running/paused with decreasing intervals,
// measuring how many updates actually make it through vs get coalesced.
func TestThroughput_StateToggle(t *testing.T) {
	skipLegacyFakeK8sHarness(t, "throughput measurement depends on fake-k8s request logging; envtest can validate final state but not raw write counts without extra instrumentation")

	env := NewTestEnv(t)
	defer env.Close()

	// Add test domain
	testUUID := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xEE, 0xFF}
	testDomain := &fakelibvirt.Domain{
		UUID:      testUUID,
		Name:      "throughput-test-vm",
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
	defer func() {
		env.cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	}()

	// Wait for initial CRD creation
	expectedName := "test-host-aabbccdd-1122-3344-5566-77889900eeff"
	_, found := env.WaitForResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName, 5*time.Second)
	if !found {
		t.Fatalf("Initial CRD not created within timeout")
	}

	// Let things stabilize
	time.Sleep(200 * time.Millisecond)

	// Clear request log to start fresh measurement
	env.FakeK8s.ClearRequests()

	// Test parameters
	const (
		nMax = 20  // number of iterations
		tMax = 200 // max sleep in milliseconds
	)

	t.Logf("Starting throughput test: %d iterations, max interval %dms", nMax, tMax)
	t.Logf("Will toggle between running <-> paused with decreasing intervals")

	type result struct {
		iteration    int
		intervalMs   int
		toggleTimeMs float64
		actualState  int
	}
	results := make([]result, 0, nMax)

	currentState := fakelibvirt.DomainRunning
	testStart := time.Now()

	for i := nMax; i >= 1; i-- {
		// Calculate sleep interval: tMax * i / nMax (decreasing)
		intervalMs := (tMax * i) / nMax
		if intervalMs < 0 {
			intervalMs = 0
		}

		// Toggle state
		if currentState == fakelibvirt.DomainRunning {
			currentState = fakelibvirt.DomainPaused
		} else {
			currentState = fakelibvirt.DomainRunning
		}

		// Measure time to issue the toggle
		toggleStart := time.Now()
		if err := env.State().SetDomainState(testUUID, uint8(currentState)); err != nil {
			t.Fatalf("SetDomainState failed at iteration %d: %v", i, err)
		}
		toggleDuration := time.Since(toggleStart)

		results = append(results, result{
			iteration:    nMax - i + 1,
			intervalMs:   intervalMs,
			toggleTimeMs: float64(toggleDuration.Microseconds()) / 1000.0,
			actualState:  currentState,
		})

		// Sleep the calculated interval
		if intervalMs > 0 {
			time.Sleep(time.Duration(intervalMs) * time.Millisecond)
		}
	}

	testDuration := time.Since(testStart)

	// Wait for pipeline to drain
	t.Log("Waiting for pipeline to drain...")
	time.Sleep(500 * time.Millisecond)

	requests := env.FakeK8s.Requests()

	// Count domain updates (PUT requests to libvirtdomains)
	domainUpdates := 0
	for _, r := range requests {
		if r.Method == "PUT" && contains(r.Path, crdmeta.LibvirtDomainResource()) {
			domainUpdates++
		}
	}

	// Calculate metrics
	totalToggles := nMax
	throughputRate := float64(domainUpdates) / testDuration.Seconds()

	// Find minimum interval where updates still got through
	// by checking the final state matches expected
	finalRes, _ := env.FakeK8s.GetResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test-ns", expectedName)
	finalK8sState := ""
	if finalRes != nil {
		if state, ok := finalRes.Status["state"].(string); ok {
			finalK8sState = state
		}
	}

	expectedFinalState := "running"
	if currentState == fakelibvirt.DomainPaused {
		expectedFinalState = "paused"
	}

	// Report results
	t.Log("")
	t.Log("============================================================")
	t.Log("THROUGHPUT TEST RESULTS")
	t.Log("============================================================")
	t.Logf("Test duration:       %v", testDuration.Round(time.Millisecond))
	t.Logf("Total toggles:       %d", totalToggles)
	t.Logf("K8s domain updates:  %d (actual writes)", domainUpdates)
	t.Logf("Effective rate:      %.2f updates/sec", throughputRate)
	t.Logf("Final expected state: %s", expectedFinalState)
	t.Logf("Final K8s state:     %s", finalK8sState)
	t.Log("------------------------------------------------------------")

	// Calculate effective minimum interval
	if domainUpdates > 0 {
		effectiveIntervalMs := testDuration.Milliseconds() / int64(domainUpdates)
		t.Logf("Effective min interval: %d ms between successful K8s writes", effectiveIntervalMs)
		t.Logf("Max sustainable rate:   %.2f state changes/sec to K8s", 1000.0/float64(effectiveIntervalMs))
	}

	t.Log("------------------------------------------------------------")
	t.Log("Per-iteration details (last 10):")
	start := 0
	if len(results) > 10 {
		start = len(results) - 10
	}
	for _, r := range results[start:] {
		t.Logf("  #%2d: interval=%3dms, toggle_time=%.3fms, target_state=%s",
			r.iteration, r.intervalMs, r.toggleTimeMs, stateString(r.actualState))
	}
	t.Log("============================================================")

	// Verify final state matches
	if finalK8sState != expectedFinalState {
		t.Errorf("Final state mismatch: K8s has %q, expected %q", finalK8sState, expectedFinalState)
	}
}

func stateString(s int) string {
	switch s {
	case fakelibvirt.DomainRunning:
		return "running"
	case fakelibvirt.DomainPaused:
		return "paused"
	case fakelibvirt.DomainShutoff:
		return "shutoff"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
