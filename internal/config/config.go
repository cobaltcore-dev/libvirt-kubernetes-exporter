// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package config manages operator configuration.
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all operator configuration.
type Config struct {
	// Libvirt settings
	// LibvirtSocket is the path to the libvirt Unix socket.
	LibvirtSocket string `yaml:"libvirtSocket"`
	// LibvirtURI is the connection URI, e.g. "qemu:///system".
	LibvirtURI string `yaml:"libvirtURI"`
	// PollInterval is the interval between libvirt polls.
	PollInterval time.Duration `yaml:"pollInterval"`

	// Kubernetes settings
	// Kubeconfig is the path to the kubeconfig file; empty means in-cluster config.
	Kubeconfig string `yaml:"kubeconfig"`

	// Identity
	// Hostname is the node name; overridden at runtime by the Downward API HOSTNAME env var.
	Hostname string `yaml:"hostname"`

	// Tuning
	// ObservationQueueSize is number of observations to buffer before blocking.
	ObservationQueueSize int `yaml:"observationQueueSize"`
	// UpdateQueueSize is the number of updates to buffer before blocking.
	UpdateQueueSize int `yaml:"updateQueueSize"`

	// Event muting — discard stale poll if a fresh event arrived recently.
	// DiscardThreshold is the duration within which a poll observation is discarded if an event was received.
	DiscardThreshold time.Duration `yaml:"discardThreshold"`

	// DeletedRetention is how long to keep domain CRDs in StateGone before the
	// delete-retention controller removes them from Kubernetes.
	DeletedRetention time.Duration `yaml:"deletedRetention"`

	// Staggered polling
	// PollRate is the target VMs/second for staggered polling (0 = no staggering).
	PollRate float64 `yaml:"pollRate"`
	// MaxPollRate is the rate cap when VM count would exceed the poll interval.
	MaxPollRate float64 `yaml:"maxPollRate"`

	// Heartbeat throttling
	// HeartbeatPeriod is how often to emit a heartbeat — every N polls (0 = always).
	HeartbeatPeriod int `yaml:"heartbeatPeriod"`

	// Rate limiting
	// HeartbeatWriteRate is the heartbeat CRD write rate in writes/sec (0 = unlimited).
	HeartbeatWriteRate float64 `yaml:"heartbeatWriteRate"`

	// DomainWriteWorkers is the maximum number of domain CRD writes that can run
	// concurrently (maps to MaxConcurrentReconciles). Individual UUIDs are still
	// serialized by the pending/stale-drop map — this controls total parallelism
	// across different domains, not per-domain concurrency.
	DomainWriteWorkers int `yaml:"domainWriteWorkers"`

	// Tracing
	// TracingEnabled enables Chrome trace output.
	TracingEnabled bool `yaml:"tracingEnabled"`
	// TracingDir is the directory for trace files.
	TracingDir string `yaml:"tracingDir"`

	// Logging
	// LogLevel sets the log verbosity: debug, info, warn, or error.
	LogLevel string `yaml:"logLevel"`
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig() *Config {
	hostname, err := os.Hostname()
	if err != nil {
		panic("failed to get hostname: " + err.Error())
	}
	return &Config{
		LibvirtSocket:        "/var/run/libvirt/libvirt-sock",
		PollInterval:         10 * time.Second,
		Hostname:             hostname,
		ObservationQueueSize: 100,
		UpdateQueueSize:      100,
		DiscardThreshold:     5 * time.Second,
		DeletedRetention:     5 * time.Minute,
		PollRate:             5.0,
		MaxPollRate:          20.0,
		HeartbeatPeriod:      6,
		HeartbeatWriteRate:   20.0,
		DomainWriteWorkers:   16,
		TracingEnabled:       false,
		TracingDir:           "./traces",
		LogLevel:             "info",
	}
}

// LoadConfig loads configuration from a YAML file, merging over defaults.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return cfg, nil
}

// ApplyEnv applies environment variable overrides for values injected by Kubernetes.
// HOSTNAME comes from the Downward API (spec.nodeName); KUBECONFIG from the pod spec.
func (c *Config) ApplyEnv() {
	if v := os.Getenv("HOSTNAME"); v != "" {
		c.Hostname = v
	}
	if v := os.Getenv("KUBECONFIG"); v != "" {
		c.Kubeconfig = v
	}
}

// BindFlags binds configuration to command-line flags for local overrides.
func (c *Config) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.LibvirtSocket, "libvirt-socket", c.LibvirtSocket,
		"Path to libvirt Unix socket")
	fs.StringVar(&c.LibvirtURI, "libvirt-uri", c.LibvirtURI,
		"Libvirt connection URI (e.g. qemu:///system)")
	fs.DurationVar(&c.PollInterval, "poll-interval", c.PollInterval,
		"Interval between libvirt polls")
	// Named kube-config (not kubeconfig) to avoid colliding with the --kubeconfig flag
	// that controller-runtime registers on flag.CommandLine at init() time.
	fs.StringVar(&c.Kubeconfig, "kube-config", c.Kubeconfig,
		"Path to kubeconfig file (uses in-cluster config if empty)")
	fs.StringVar(&c.Hostname, "hostname", c.Hostname,
		"Hostname identifier for this operator instance")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel,
		"Log level (debug, info, warn, error)")
	fs.IntVar(&c.ObservationQueueSize, "obs-queue-size", c.ObservationQueueSize,
		"Size of observation channel buffer")
	fs.IntVar(&c.UpdateQueueSize, "update-queue-size", c.UpdateQueueSize,
		"Size of update channel buffer")
	fs.DurationVar(&c.DiscardThreshold, "discard-threshold", c.DiscardThreshold,
		"Discard poll observation if event arrived within this duration")
	fs.DurationVar(&c.DeletedRetention, "deleted-retention", c.DeletedRetention,
		"How long to retain the CRD for a deleted VM (StateGone) before removing it from Kubernetes")
}

// Validate checks configuration for errors.
func (c *Config) Validate() error {
	if c.LibvirtSocket == "" {
		return errors.New("libvirt-socket is required")
	}
	if c.LibvirtURI == "" {
		return errors.New("libvirt-uri is required")
	}
	if c.Hostname == "" {
		return errors.New("hostname is required")
	}
	if c.PollInterval < 10*time.Millisecond {
		return errors.New("poll-interval must be at least 10ms")
	}
	if c.ObservationQueueSize < 1 {
		return errors.New("obs-queue-size must be at least 1")
	}
	if c.UpdateQueueSize < 1 {
		return errors.New("update-queue-size must be at least 1")
	}
	return nil
}
