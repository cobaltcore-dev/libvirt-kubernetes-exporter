// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
)

func TestDefaultTracingDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.TracingEnabled {
		t.Error("tracing should be disabled by default")
	}
}

func TestLoadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
libvirtSocket: /tmp/test.sock
libvirtURI: qemu:///system
pollInterval: 5s
pollRate: 10.0
tracingEnabled: true
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.LibvirtSocket != "/tmp/test.sock" {
		t.Errorf("expected libvirtSocket /tmp/test.sock, got %s", cfg.LibvirtSocket)
	}
	if cfg.PollRate != 10.0 {
		t.Errorf("expected pollRate 10.0, got %v", cfg.PollRate)
	}
	if !cfg.TracingEnabled {
		t.Error("expected tracingEnabled true")
	}
}

func TestLoadYAMLMergesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Only override one field; all others should retain defaults
	if err := os.WriteFile(configPath, []byte("pollRate: 7.0\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.PollRate != 7.0 {
		t.Errorf("expected pollRate 7.0, got %v", cfg.PollRate)
	}
	if cfg.PollRate == 5.0 {
		t.Errorf("expected overridden pollRate, got default 5.0")
	}
}

func TestApplyEnvOverridesHostname(t *testing.T) {
	t.Setenv("HOSTNAME", "test-node-42")

	cfg := config.DefaultConfig()
	cfg.ApplyEnv()

	if cfg.Hostname != "test-node-42" {
		t.Errorf("expected hostname test-node-42, got %s", cfg.Hostname)
	}
}
