// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package testharness provides an integrated test environment for the operator.
//
// It orchestrates:
// - fake-libvirt: simulated libvirt daemon
// - fake-k8s: simulated Kubernetes API
// - stepping scheduler: deterministic goroutine control
// - operator under test
package testharness

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/step"
)

// Harness is the test orchestrator.
type Harness struct {
	mu sync.Mutex

	// Components
	FakeK8s   *fakek8s.Server
	Scheduler *step.SteppingScheduler

	// Libvirt socket
	libvirtListener net.Listener
	libvirtPath     string

	// Configuration
	k8sNamespace string

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds harness configuration.
type Config struct {
	K8sNamespace string
}

// New creates a new test harness.
func New(cfg Config) (*Harness, error) {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Harness{
		FakeK8s:      fakek8s.New(),
		Scheduler:    step.NewSteppingScheduler(),
		k8sNamespace: cfg.K8sNamespace,
		ctx:          ctx,
		cancel:       cancel,
	}

	if h.k8sNamespace == "" {
		h.k8sNamespace = "test"
	}

	// Create a temporary socket for fake-libvirt
	sockDir, err := os.MkdirTemp("", "harness-libvirt-*")
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	h.libvirtPath = sockDir + "/libvirt.sock"

	return h, nil
}

// StartFakeLibvirt starts the fake libvirt daemon on the configured socket.
// It accepts a handler function that will be called for each connection.
// The handler should implement the libvirt RPC protocol.
func (h *Harness) StartFakeLibvirt(handler func(net.Conn)) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.libvirtListener != nil {
		return fmt.Errorf("fake-libvirt already started")
	}

	listener, err := net.Listen("unix", h.libvirtPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", h.libvirtPath, err)
	}
	h.libvirtListener = listener

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Listener closed
				return
			}
			go handler(conn)
		}
	}()

	return nil
}

// LibvirtSocket returns the path to the fake libvirt socket.
func (h *Harness) LibvirtSocket() string {
	return h.libvirtPath
}

// K8sServer returns the URL of the fake Kubernetes API server.
func (h *Harness) K8sServer() string {
	return h.FakeK8s.URL()
}

// K8sNamespace returns the configured namespace.
func (h *Harness) K8sNamespace() string {
	return h.k8sNamespace
}

// Context returns the harness context.
func (h *Harness) Context() context.Context {
	return h.ctx
}

// Close shuts down all harness components.
func (h *Harness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cancel()

	if h.libvirtListener != nil {
		h.libvirtListener.Close()
		h.libvirtListener = nil
	}

	if h.FakeK8s != nil {
		h.FakeK8s.Close()
	}

	// Wait for goroutines
	h.wg.Wait()

	// Cleanup socket directory
	if h.libvirtPath != "" {
		os.RemoveAll(h.libvirtPath[:len(h.libvirtPath)-len("/libvirt.sock")])
	}
}

// WaitFor blocks until a worker arrives at the specified step point.
func (h *Harness) WaitFor(workerID string, timeout time.Duration) (*step.Arrival, error) {
	return h.Scheduler.WaitForWorker(h.ctx, workerID, timeout)
}

// WaitForPoint blocks until any worker arrives at the specified step point.
func (h *Harness) WaitForPoint(point step.Point, timeout time.Duration) (*step.Arrival, error) {
	return h.Scheduler.WaitForPoint(h.ctx, point, timeout)
}

// Release allows a worker to continue from its step point.
func (h *Harness) Release(workerID string) error {
	return h.Scheduler.Release(workerID)
}

// ReleaseAll allows all waiting workers to continue.
func (h *Harness) ReleaseAll() {
	h.Scheduler.ReleaseAll()
}

// Waiting returns all workers currently blocked at step points.
func (h *Harness) Waiting() []step.Arrival {
	return h.Scheduler.Waiting()
}

// StepLog returns the log of all step events.
func (h *Harness) StepLog() []step.LogEntry {
	return h.Scheduler.Log()
}

// SetK8sFault injects a fault for Kubernetes operations.
func (h *Harness) SetK8sFault(pathPattern string, fault *fakek8s.FaultConfig) {
	h.FakeK8s.SetFault(pathPattern, fault)
}

// GetK8sResource retrieves a resource from the fake Kubernetes store.
func (h *Harness) GetK8sResource(group, version, resource, namespace, name string) (*fakek8s.Resource, bool) {
	return h.FakeK8s.GetResource(group, version, resource, namespace, name)
}

// K8sRequests returns all requests made to the fake Kubernetes server.
func (h *Harness) K8sRequests() []fakek8s.RequestLog {
	return h.FakeK8s.Requests()
}

// Reset clears all state for a fresh test.
func (h *Harness) Reset() {
	h.FakeK8s.Reset()
	h.Scheduler.ReleaseAll()
}
