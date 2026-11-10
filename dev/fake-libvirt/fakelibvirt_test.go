// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package fakelibvirt

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalocean/go-libvirt"
)

func TestServer_GoLibvirtIntegration(t *testing.T) {
	// Create temp socket
	tmpDir, err := os.MkdirTemp("", "fakelibvirt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	socketPath := filepath.Join(tmpDir, "libvirt.sock")

	// Start server with default test domains
	server := NewServer(nil)
	if err := server.Listen(socketPath); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Close()

	// Connect with go-libvirt
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	l := libvirt.New(conn)
	if err := l.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer l.Disconnect()

	// Test: Get version
	ver, err := l.ConnectGetLibVersion()
	if err != nil {
		t.Fatalf("ConnectGetLibVersion failed: %v", err)
	}
	if ver != 10001000 {
		t.Errorf("unexpected version: %d", ver)
	}

	// Test: List domains
	domains, count, err := l.ConnectListAllDomains(1, 0)
	if err != nil {
		t.Fatalf("ConnectListAllDomains failed: %v", err)
	}
	if count != 3 { // default test state has 3 domains
		t.Errorf("expected 3 domains, got %d", count)
	}
	if len(domains) != 3 {
		t.Errorf("expected 3 domain structs, got %d", len(domains))
	}

	// Test: Get domain info
	for _, dom := range domains {
		state, maxMem, mem, nrVCPU, cpuTime, err := l.DomainGetInfo(dom)
		if err != nil {
			t.Errorf("DomainGetInfo failed for %s: %v", dom.Name, err)
			continue
		}
		t.Logf("Domain %s: state=%d maxMem=%d mem=%d vcpu=%d cpuTime=%d",
			dom.Name, state, maxMem, mem, nrVCPU, cpuTime)
	}
}

func TestServer_LifecycleEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fakelibvirt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	socketPath := filepath.Join(tmpDir, "libvirt.sock")

	// Start with empty state
	st := NewEmpty()
	server := NewServer(st)
	if err := server.Listen(socketPath); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Close()

	// Connect
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	l := libvirt.New(conn)
	if err := l.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer l.Disconnect()

	// Register for lifecycle events
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := l.LifecycleEvents(ctx)
	if err != nil {
		t.Fatalf("LifecycleEvents failed: %v", err)
	}

	// Add a domain (should trigger Defined event)
	testDomain := &Domain{
		UUID:      [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00},
		Name:      "test-event-vm",
		ID:        -1,
		State:     DomainShutoff,
		MaxMem:    1048576,
		Memory:    0,
		NrVirtCPU: 1,
	}

	// Give a moment for the event listener to register
	time.Sleep(50 * time.Millisecond)

	if err := st.AddDomain(testDomain); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	// Should receive Defined event
	select {
	case event := <-events:
		t.Logf("Received event: %s event=%d", event.Dom.Name, event.Event)
		if event.Event != int32(DomainEventDefined) {
			t.Errorf("expected Defined event (0), got %d", event.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Defined event")
	}

	// Start the domain (transition to running)
	if err := st.SetDomainState(testDomain.UUID, DomainRunning); err != nil {
		t.Fatalf("SetDomainState failed: %v", err)
	}

	// Should receive Started event
	select {
	case event := <-events:
		t.Logf("Received event: %s event=%d", event.Dom.Name, event.Event)
		if event.Event != int32(DomainEventStarted) {
			t.Errorf("expected Started event (2), got %d", event.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Started event")
	}

	// Stop the domain
	if err := st.SetDomainState(testDomain.UUID, DomainShutoff); err != nil {
		t.Fatalf("SetDomainState failed: %v", err)
	}

	// Should receive Stopped event
	select {
	case event := <-events:
		t.Logf("Received event: %s event=%d", event.Dom.Name, event.Event)
		if event.Event != int32(DomainEventStopped) {
			t.Errorf("expected Stopped event (5), got %d", event.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Stopped event")
	}

	// Remove the domain
	if err := st.RemoveDomain(testDomain.UUID); err != nil {
		t.Fatalf("RemoveDomain failed: %v", err)
	}

	// Should receive Undefined event
	select {
	case event := <-events:
		t.Logf("Received event: %s event=%d", event.Dom.Name, event.Event)
		if event.Event != int32(DomainEventUndefined) {
			t.Errorf("expected Undefined event (1), got %d", event.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Undefined event")
	}
}

func TestServer_MultipleConnections(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fakelibvirt-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	socketPath := filepath.Join(tmpDir, "libvirt.sock")

	server := NewServer(nil)
	if err := server.Listen(socketPath); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Close()

	// Create multiple connections
	var clients []*libvirt.Libvirt
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to dial connection %d: %v", i, err)
		}
		defer conn.Close()

		l := libvirt.New(conn)
		if err := l.Connect(); err != nil {
			t.Fatalf("Failed to connect %d: %v", i, err)
		}
		defer l.Disconnect()
		clients = append(clients, l)
	}

	// All should be able to list domains
	for i, l := range clients {
		_, count, err := l.ConnectListAllDomains(1, 0)
		if err != nil {
			t.Errorf("Client %d: ListAllDomains failed: %v", i, err)
		}
		if count != 3 {
			t.Errorf("Client %d: expected 3 domains, got %d", i, count)
		}
	}
}
