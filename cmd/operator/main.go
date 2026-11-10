// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package main provides the CLI entry point for the libvirt-kubernetes operator.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/operator"
)

func main() {
	configFile := configFileFromArgs(os.Args[1:])

	// Load configuration from file or use defaults
	var cfg *config.Config
	if configFile != "" {
		var err error
		cfg, err = config.LoadConfig(configFile)
		if err != nil {
			slog.Error("load config file", "err", err)
			os.Exit(1)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	// HOSTNAME is injected by the Downward API; KUBECONFIG may be set in the pod spec
	cfg.ApplyEnv()

	// CLI flags override everything (re-bind with current cfg values as defaults)
	flag.StringVar(&configFile, "config", configFile, "Path to YAML config file (ConfigMap mount)")
	cfg.BindFlags(flag.CommandLine)
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}

	logging.Setup(os.Stderr, cfg.LogLevel)

	// Wire controller-runtime's internal logger to slog so its cache/watch/reconcile
	// logs flow through the same pipeline and level as the rest of the operator.
	ctrl.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))

	slog.Info("libvirt-kubernetes-exporter starting",
		"hostname", cfg.Hostname,
		"libvirt_socket", cfg.LibvirtSocket,
	)

	op, err := operator.New(cfg)
	if err != nil {
		slog.Error("failed to create operator", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	if err := op.Run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("operator error", "err", err)
		os.Exit(1)
	}

	slog.Info("libvirt-kubernetes-exporter stopped")
}

func configFileFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--config" || arg == "-config" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if v, ok := strings.CutPrefix(arg, "--config="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(arg, "-config="); ok {
			return v
		}
	}
	return ""
}
