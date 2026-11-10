// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package main provides a combined demo server for the web UI.
// It starts all 4 API servers (fake-libvirt, fake-k8s, operator, harness)
// and optionally the operator itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	operatorapi "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/integration/operatorapi"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/operator"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	harnessPort  = flag.Int("harness-port", 8080, "Harness API port")
	libvirtPort  = flag.Int("libvirt-port", 8081, "fake-libvirt API port")
	k8sPort      = flag.Int("k8s-port", 8082, "fake-k8s API port")
	operatorPort = flag.Int("operator-port", 8083, "Operator API port")
	webuiPort    = flag.Int("webui-port", 8000, "Web UI static file server port")
	webuiDir     = flag.String("webui-dir", "", "Path to web UI directory")
	runOperator  = flag.Bool("run-operator", true, "Run the operator (connects libvirt to k8s)")
)

func main() {
	flag.Parse()

	slog.Info("starting lv2k8s combined demo")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create temp directory for sockets
	tmpDir, err := os.MkdirTemp("", "lv2k8s-demo-*")
	if err != nil {
		slog.Error("failed to create temp dir", "err", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "libvirt.sock")

	// Create shared components
	libvirtState := fakelibvirt.NewEmpty()
	addSampleDomains(libvirtState)

	fakeK8s := fakek8s.New()

	// Start fake-libvirt socket server
	libvirtServer := fakelibvirt.NewServer(libvirtState)
	if err := libvirtServer.Listen(socketPath); err != nil {
		slog.Error("failed to start fake-libvirt socket", "err", err)
		os.Exit(1)
	}
	defer libvirtServer.Close()
	slog.Info("fake-libvirt socket listening", "socket", socketPath)

	// Start fake-libvirt API
	libvirtAPI := fakelibvirt.NewAPIServer(libvirtState)
	go func() {
		addr := fmt.Sprintf(":%d", *libvirtPort)
		slog.Info("fake-libvirt API listening", "addr", addr)
		if err := libvirtAPI.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("fake-libvirt API error", "err", err)
		}
	}()

	// Start fake-k8s API
	k8sAPI := fakek8s.NewAPIServer(fakeK8s)
	go func() {
		addr := fmt.Sprintf(":%d", *k8sPort)
		slog.Info("fake-k8s API listening", "addr", addr)
		if err := k8sAPI.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("fake-k8s API error", "err", err)
		}
	}()

	// Create operator config
	cfg := &config.Config{
		LibvirtSocket:        socketPath,
		LibvirtURI:           "qemu:///system",
		PollInterval:         5 * time.Second,
		Hostname:             "demo-host",
		ObservationQueueSize: 100,
		UpdateQueueSize:      100,
		DiscardThreshold:     3 * time.Second,  // event muting
		DeletedRetention:     30 * time.Second, // keep deleted VMs visible for 30s
		LogLevel:             "info",
	}

	// Create k8s client
	k8sConfig := &rest.Config{Host: fakeK8s.URL()}
	dynClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		slog.Error("failed to create k8s client", "err", err)
		os.Exit(1)
	}

	// Create operator (but don't run yet)
	op, err := operator.NewWithExporter(cfg, dynClient)
	if err != nil {
		slog.Error("failed to create operator", "err", err)
		os.Exit(1)
	}

	// Start operator API using the operator's actual tracker and queue
	operatorAPIServer := operatorapi.NewServer(cfg, op.Tracker(), nil)

	// Wire up tracker events to SSE broadcasting
	op.Tracker().SetEventCallback(func(eventType string, domain *types.DomainState) {
		operatorAPIServer.BroadcastEvent(eventType, domain)
	})

	// Wire up tracing for tracker and exporter events
	traceCallback := func(cat, name, phase string, args map[string]any) {
		operatorAPIServer.TraceEvent(cat, name, phase, args)
	}
	op.Tracker().SetTraceCallback(traceCallback)
	op.Exporter().SetTraceCallback(traceCallback)

	// Route slog output through the operator API for SSE streaming
	// LogWriter already tees to the original stderr internally
	logging.Setup(operatorAPIServer.LogWriter(), cfg.LogLevel)

	go func() {
		addr := fmt.Sprintf(":%d", *operatorPort)
		slog.Info("operator API listening", "addr", addr)
		if err := operatorAPIServer.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("operator API error", "err", err)
		}
	}()

	// Determine repo root for test discovery
	repoRoot := findRepoRoot()

	// Create and start harness API with test runner
	harnessAPI := NewHarnessServer(libvirtState, fakeK8s, socketPath, operatorAPIServer.TraceStore(), repoRoot)
	go func() {
		addr := fmt.Sprintf(":%d", *harnessPort)
		slog.Info("harness API listening", "addr", addr)
		if err := harnessAPI.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("harness API error", "err", err)
		}
	}()

	// Run the operator
	if *runOperator {
		go func() {
			slog.Info("operator started")
			if err := op.Run(ctx); err != nil && ctx.Err() == nil {
				slog.Error("operator error", "err", err)
			}
		}()
	}

	// Serve web UI if specified
	if *webuiDir != "" {
		go func() {
			addr := fmt.Sprintf(":%d", *webuiPort)
			slog.Info("web UI serving", "dir", *webuiDir, "addr", addr)
			fs := http.FileServer(http.Dir(*webuiDir))
			if err := http.ListenAndServe(addr, fs); err != nil {
				slog.Error("web UI server error", "err", err)
			}
		}()
	}

	printBanner(*webuiPort, *harnessPort, *libvirtPort, *k8sPort, *operatorPort, *webuiDir)

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down")
	cancel()
}

func addSampleDomains(st *fakelibvirt.FakeState) {
	st.AddDomain(&fakelibvirt.Domain{
		Name:      "web-server-01",
		UUID:      [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		ID:        1,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    4194304,
		Memory:    2097152,
		NrVirtCPU: 4,
	})

	st.AddDomain(&fakelibvirt.Domain{
		Name:      "database-01",
		UUID:      [16]byte{0x02, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		ID:        2,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    8388608,
		Memory:    8388608,
		NrVirtCPU: 8,
	})

	st.AddDomain(&fakelibvirt.Domain{
		Name:      "worker-01",
		UUID:      [16]byte{0x03, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		ID:        -1,
		State:     fakelibvirt.DomainShutoff,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	})

	slog.Info("added sample domains", "count", 3)
}

// findRepoRoot finds the repository root by looking for .git directory.
func findRepoRoot() string {
	// Start from current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}

	// Walk up looking for .git
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	// Fallback: try common relative paths from integration/cmd/demo
	candidates := []string{
		"../../..", // from integration/cmd/demo
		"../..",    // from integration/cmd
		"..",       // from integration
		".",        // current dir
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, ".git")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}

	return "."
}

func printBanner(webuiPort, harnessPort, libvirtPort, k8sPort, operatorPort int, webuiDir string) {
	slog.Info("========================================")
	if webuiDir != "" {
		slog.Info(fmt.Sprintf("  Web UI:       http://localhost:%d", webuiPort))
	}
	slog.Info(fmt.Sprintf("  Harness API:  http://localhost:%d", harnessPort))
	slog.Info(fmt.Sprintf("  Libvirt API:  http://localhost:%d", libvirtPort))
	slog.Info(fmt.Sprintf("  K8s API:      http://localhost:%d", k8sPort))
	slog.Info(fmt.Sprintf("  Operator API: http://localhost:%d", operatorPort))
	slog.Info("========================================")
	slog.Info("Press Ctrl+C to stop.")
}
