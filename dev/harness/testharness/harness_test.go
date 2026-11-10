// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package testharness

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
)

func TestHarness_Lifecycle(t *testing.T) {
	h, err := New(Config{K8sNamespace: "test-ns"})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	// Verify socket path exists
	if h.LibvirtSocket() == "" {
		t.Error("LibvirtSocket is empty")
	}

	// Verify k8s server is running
	resp, err := http.Get(h.K8sServer() + "/apis/test/v1/tests")
	if err != nil {
		t.Fatalf("K8s server not reachable: %v", err)
	}
	resp.Body.Close()

	// Verify scheduler is enabled
	if !h.Scheduler.Enabled() {
		t.Error("Scheduler should be enabled")
	}
}

func TestHarness_K8sCRUD(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	// Create a resource
	h.FakeK8s.SetResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test", &fakek8s.Resource{
		APIVersion: crdmeta.APIVersion(),
		Kind:       crdmeta.LibvirtDomainKind(),
		Metadata: fakek8s.Metadata{
			Name:      "domain-1",
			Namespace: "test",
		},
		Status: map[string]any{
			"state": "running",
		},
	})

	// Retrieve it
	res, ok := h.GetK8sResource(crdmeta.Group, crdmeta.Version, crdmeta.LibvirtDomainResource(), "test", "domain-1")
	if !ok {
		t.Fatal("resource not found")
	}
	if res.Status["state"] != "running" {
		t.Errorf("wrong state: %v", res.Status["state"])
	}
}

func TestHarness_Stepping(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	ctx := h.Context()

	// Start a worker that will step
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Scheduler.Step(ctx, "worker-1", "test:step-1")
		h.Scheduler.Step(ctx, "worker-1", "test:step-2")
	}()

	// Wait for first step
	arrival, err := h.WaitFor("worker-1", time.Second)
	if err != nil {
		t.Fatalf("WaitFor failed: %v", err)
	}
	if arrival.Point != "test:step-1" {
		t.Errorf("wrong point: %s", arrival.Point)
	}

	// Release first step
	if err := h.Release("worker-1"); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Wait for second step
	arrival, err = h.WaitFor("worker-1", time.Second)
	if err != nil {
		t.Fatalf("WaitFor second failed: %v", err)
	}
	if arrival.Point != "test:step-2" {
		t.Errorf("wrong point: %s", arrival.Point)
	}

	// Release and wait for completion
	h.Release("worker-1")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not complete")
	}

	// Check step log
	log := h.StepLog()
	if len(log) != 4 { // 2 arrives + 2 releases
		t.Errorf("expected 4 log entries, got %d", len(log))
	}
}

func TestHarness_FaultInjection(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	// Inject fault
	h.SetK8sFault(crdmeta.BaseLibvirtDomainResource, &fakek8s.FaultConfig{
		Code:     http.StatusServiceUnavailable,
		ErrorMsg: "service unavailable",
	})

	// Try to access
	url := h.K8sServer() + "/apis/" + crdmeta.Group + "/" + crdmeta.Version + "/namespaces/test/" + crdmeta.LibvirtDomainResource()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}

	// Clear fault
	h.SetK8sFault(crdmeta.BaseLibvirtDomainResource, nil)

	resp, _ = http.Get(url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after clearing fault, got %d", resp.StatusCode)
	}
}

func TestHarness_MultipleWorkers(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	ctx := h.Context()

	// Start multiple workers
	for i := 0; i < 3; i++ {
		go func(id int) {
			h.Scheduler.Step(ctx, workerID(id), "test:parallel")
		}(i)
	}

	// Wait for all to arrive
	for i := 0; i < 3; i++ {
		_, err := h.WaitFor(workerID(i), time.Second)
		if err != nil {
			t.Fatalf("WaitFor worker%d failed: %v", i, err)
		}
	}

	// All should be waiting
	waiting := h.Waiting()
	if len(waiting) != 3 {
		t.Errorf("expected 3 waiting, got %d", len(waiting))
	}

	// Release in specific order
	order := []int{2, 0, 1}
	for _, i := range order {
		if err := h.Release(workerID(i)); err != nil {
			t.Fatalf("Release worker%d failed: %v", i, err)
		}
		// Brief delay to allow worker to proceed
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHarness_RequestLog(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	// Make some requests
	url := h.K8sServer() + "/apis/" + crdmeta.Group + "/" + crdmeta.Version + "/namespaces/test/" + crdmeta.LibvirtDomainResource()
	http.Get(url)

	http.Post(url, "application/json", nil)

	// Check log
	reqs := h.K8sRequests()
	if len(reqs) < 2 {
		t.Errorf("expected at least 2 requests, got %d", len(reqs))
	}
}

func TestHarness_FakeLibvirt(t *testing.T) {
	h, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer h.Close()

	// Start fake libvirt with a simple handler
	received := make(chan []byte, 1)
	err = h.StartFakeLibvirt(func(conn net.Conn) {
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if n > 0 {
			received <- buf[:n]
		}
		conn.Close()
	})
	if err != nil {
		t.Fatalf("StartFakeLibvirt failed: %v", err)
	}

	// Connect to it
	conn, err := net.Dial("unix", h.LibvirtSocket())
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Send some data
	testData := []byte("hello fake-libvirt")
	conn.Write(testData)

	select {
	case data := <-received:
		if string(data) != string(testData) {
			t.Errorf("wrong data received: %s", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for data")
	}
}

func workerID(i int) string {
	return "worker" + string(rune('0'+i))
}
