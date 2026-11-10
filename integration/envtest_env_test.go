// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/operator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// envtestEnv wraps a real operator running against envtest kube-apiserver.
type envtestEnv struct {
	FakeLibvirt *fakelibvirt.Server
	Operator    *operator.Operator
	DynClient   dynamic.Interface

	tmpDir string
	ctx    context.Context
	cancel context.CancelFunc
}

// newEnvtestEnv creates an envtestEnv. Tests skip when KUBEBUILDER_ASSETS is absent.
func newEnvtestEnv(t *testing.T) *envtestEnv {
	t.Helper()
	if envtestCfg == nil {
		t.Skip("KUBEBUILDER_ASSETS not set — skipping envtest tests")
	}

	tmpDir, err := os.MkdirTemp("", "envtest-*")
	if err != nil {
		t.Fatalf("create tmpdir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	fakeLibvirt := fakelibvirt.NewServer(fakelibvirt.NewEmpty())
	socketPath := filepath.Join(tmpDir, "libvirt.sock")
	if err := fakeLibvirt.Listen(socketPath); err != nil {
		cancel()
		os.RemoveAll(tmpDir)
		t.Fatalf("start fake-libvirt: %v", err)
	}

	cfg := &config.Config{
		LibvirtSocket:        socketPath,
		LibvirtURI:           "qemu:///system",
		PollInterval:         100 * time.Millisecond,
		Hostname:             "test-host",
		ObservationQueueSize: 100,
		UpdateQueueSize:      100,
		DiscardThreshold:     5 * time.Second,
		DeletedRetention:     100 * time.Millisecond, // short for retention tests
		DomainWriteWorkers:   4,
	}

	op, err := operator.NewWithRestConfig(cfg, envtestCfg)
	if err != nil {
		cancel()
		fakeLibvirt.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("create operator: %v", err)
	}

	dynClient, err := dynamic.NewForConfig(envtestCfg)
	if err != nil {
		cancel()
		fakeLibvirt.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("create dyn client: %v", err)
	}

	env := &envtestEnv{
		FakeLibvirt: fakeLibvirt,
		Operator:    op,
		DynClient:   dynClient,
		tmpDir:      tmpDir,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start operator in background; done is closed when it exits.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := op.Run(ctx); err != nil && ctx.Err() == nil {
			t.Logf("operator exited: %v", err)
		}
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		fakeLibvirt.Close()
		os.RemoveAll(tmpDir)
		// Clean up all CRDs so the shared envtest API server is left empty for the next test.
		cleanupCtx := context.Background()
		_ = dynClient.Resource(domainGVR2).DeleteCollection(cleanupCtx, metav1.DeleteOptions{}, metav1.ListOptions{})
		_ = dynClient.Resource(heartbeatGVR2).DeleteCollection(cleanupCtx, metav1.DeleteOptions{}, metav1.ListOptions{})
	})

	return env
}

// addDomain adds a domain to fake-libvirt and returns its UUID string.
func (e *envtestEnv) addDomain(t *testing.T, uuid [16]byte, name string) string {
	t.Helper()
	d := &fakelibvirt.Domain{
		UUID:      uuid,
		Name:      name,
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	}
	if err := e.FakeLibvirt.State().AddDomain(d); err != nil {
		t.Fatalf("AddDomain(%s): %v", name, err)
	}
	return formatUUID(uuid)
}

func formatUUID(b [16]byte) string {
	const hex = "0123456789abcdef"
	buf := make([]byte, 36)
	groups := [][2]int{{0, 4}, {4, 6}, {6, 8}, {8, 10}, {10, 16}}
	pos := 0
	for gi, g := range groups {
		for i := g[0]; i < g[1]; i++ {
			buf[pos] = hex[b[i]>>4]
			buf[pos+1] = hex[b[i]&0xf]
			pos += 2
		}
		if gi < len(groups)-1 {
			buf[pos] = '-'
			pos++
		}
	}
	return string(buf)
}

var domainGVR2 = schema.GroupVersionResource{
	Group:    "kvm.cloud.sap",
	Version:  "v1alpha1",
	Resource: "libvirtdomains",
}
var heartbeatGVR2 = schema.GroupVersionResource{
	Group:    "kvm.cloud.sap",
	Version:  "v1alpha1",
	Resource: "libvirtdomainheartbeats",
}

// waitForDomain polls until the domain CRD appears (or the timeout fires).
func (e *envtestEnv) waitForDomain(t *testing.T, uuid string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := e.DynClient.Resource(domainGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("domain CRD %q not found within %v", name, timeout)
}

// waitForDomainStatus polls until the domain CRD's status.state equals want.
func (e *envtestEnv) waitForDomainStatus(t *testing.T, uuid, want string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		obj, err := e.DynClient.Resource(domainGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err == nil {
			st, _, _ := unstructured.NestedString(obj.Object, "status", "state")
			if st == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("domain CRD %q status.state did not reach %q within %v", name, want, timeout)
}

// waitForDomainGone polls until the domain CRD is absent (deleted).
func (e *envtestEnv) waitForDomainGone(t *testing.T, uuid string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := e.DynClient.Resource(domainGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return // not found — gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("domain CRD %q still exists after %v", name, timeout)
}

// waitForHeartbeat polls until the heartbeat CRD appears.
func (e *envtestEnv) waitForHeartbeat(t *testing.T, uuid string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := e.DynClient.Resource(heartbeatGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("heartbeat CRD %q not found within %v", name, timeout)
}
