// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package operator provides the main orchestration for the libvirt-kubernetes exporter.
package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	ctrlevent "sigs.k8s.io/controller-runtime/pkg/event"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/controllers"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/exporter"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/state"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	metav1.AddToGroupVersion(scheme, v1alpha1.GroupVersion)
	// Add clientgoscheme.AddToScheme(scheme) once we use leader election,
	// events, or other controller-runtime features that touch core K8s types.
}

var logger = logging.For("operator")

// Operator orchestrates the libvirt observation and Kubernetes export pipeline.
type Operator struct {
	config *config.Config

	obsCh         chan types.Observation
	updateCh      chan types.Update
	updateEventCh chan ctrlevent.TypedGenericEvent[*types.Update]

	observer *libvirt.Observer
	tracker  *state.Tracker
	exporter *exporter.Exporter
	manager  ctrl.Manager
}

// New creates a new operator with the given configuration.
func New(cfg *config.Config) (*Operator, error) {
	restCfg, err := buildRestConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}
	return newWithRestConfig(cfg, restCfg, false)
}

// NewWithRestConfig creates an operator using the supplied REST config.
// Intended for integration tests that provide an envtest REST config directly.
// It sets SkipNameValidation so multiple operators can be created in one process.
func NewWithRestConfig(cfg *config.Config, restCfg *rest.Config) (*Operator, error) {
	return newWithRestConfig(cfg, restCfg, true)
}

func newWithRestConfig(cfg *config.Config, restCfg *rest.Config, skipNameValidation bool) (*Operator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	obsCh := make(chan types.Observation, cfg.ObservationQueueSize)
	updateCh := make(chan types.Update, cfg.UpdateQueueSize)
	updateEventCh := make(chan ctrlevent.TypedGenericEvent[*types.Update], cfg.UpdateQueueSize)

	observer := libvirt.NewObserver(cfg.LibvirtSocket, cfg.Hostname, obsCh)
	observer.SetPollInterval(cfg.PollInterval)
	observer.SetLibvirtURI(cfg.LibvirtURI)

	tracker := state.NewTrackerWithConfig(cfg.Hostname, updateCh, state.TrackerConfig{
		DiscardThreshold: cfg.DiscardThreshold,
		HeartbeatPeriod:  cfg.HeartbeatPeriod,
	})

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	exp := exporter.NewExporterWithConfig(dynClient, exporter.ExporterConfig{
		Hostname:           cfg.Hostname,
		HeartbeatTTL:       heartbeatTTL(cfg),
		HeartbeatWriteRate: cfg.HeartbeatWriteRate,
	})

	// To log controller-runtime output via slog, ctrl.SetLogger must be called
	// before New() so it picks up the configured handler.
	// SkipNameValidation allows multiple managers to be created in the same
	// process (e.g. in tests), where the global controller name registry would
	// otherwise reject re-registering the same names.
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Controller: ctrlconfig.Controller{
			SkipNameValidation: &skipNameValidation,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create manager: %w", err)
	}

	if err := (&controllers.DomainWriteController{
		Client:     mgr.GetClient(),
		KnownStore: exp,
		Workers:    cfg.DomainWriteWorkers,
	}).SetupWithManager(mgr, updateEventCh); err != nil {
		return nil, fmt.Errorf("setup domain-write controller: %w", err)
	}

	if err := (&controllers.DriftDetectionController{
		Client:      mgr.GetClient(),
		Tracker:     tracker,
		Invalidator: exp,
		Hostname:    cfg.Hostname,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setup drift-detection controller: %w", err)
	}

	if err := (&controllers.DeleteRetentionController{
		Client:          mgr.GetClient(),
		RetentionPeriod: cfg.DeletedRetention,
		Hostname:        cfg.Hostname,
	}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setup delete-retention controller: %w", err)
	}

	return &Operator{
		config:        cfg,
		obsCh:         obsCh,
		updateCh:      updateCh,
		updateEventCh: updateEventCh,
		observer:      observer,
		tracker:       tracker,
		exporter:      exp,
		manager:       mgr,
	}, nil
}

// NewWithExporter creates an operator with a custom exporter client (for testing).
//
// TODO: this legacy fake-k8s test constructor predates the controller-runtime
// domain-write path. It injects a dynamic client for direct exporter writes, but
// does not create a manager, updateEventCh, or DomainWriteController. Since the
// exporter now writes only heartbeat CRDs, operators built this way do not write
// domain CRDs. Keep this constructor only as a marker for the fake-k8s harness
// until that harness is either ported to envtest/controller-runtime or replaced
// with another fault-injection path.
func NewWithExporter(cfg *config.Config, client dynamic.Interface) (*Operator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	obsCh := make(chan types.Observation, cfg.ObservationQueueSize)
	updateCh := make(chan types.Update, cfg.UpdateQueueSize)

	observer := libvirt.NewObserver(cfg.LibvirtSocket, cfg.Hostname, obsCh)
	observer.SetPollInterval(cfg.PollInterval)
	observer.SetLibvirtURI(cfg.LibvirtURI)

	tracker := state.NewTrackerWithConfig(cfg.Hostname, updateCh, state.TrackerConfig{
		DiscardThreshold: cfg.DiscardThreshold,
		HeartbeatPeriod:  cfg.HeartbeatPeriod,
	})

	exp := exporter.NewExporterWithConfig(client, exporter.ExporterConfig{
		Hostname:     cfg.Hostname,
		HeartbeatTTL: heartbeatTTL(cfg),
	})

	return &Operator{
		config:   cfg,
		obsCh:    obsCh,
		updateCh: updateCh,
		observer: observer,
		tracker:  tracker,
		exporter: exp,
	}, nil
}

// Run starts the operator and blocks until the context is cancelled.
func (o *Operator) Run(ctx context.Context) error {
	logger.Info("starting",
		"hostname", o.config.Hostname,
		"libvirt_socket", o.config.LibvirtSocket,
		"libvirt_uri", o.config.LibvirtURI,
		"poll_interval", o.config.PollInterval,
	)

	var wg sync.WaitGroup
	errCh := make(chan error, 4)

	if o.manager != nil {
		wg.Go(func() {
			if err := o.manager.Start(ctx); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("manager: %w", err)
			}
		})
		if !o.manager.GetCache().WaitForCacheSync(ctx) {
			return ctx.Err()
		}
		logger.Info("manager cache synced")
	}

	wg.Go(func() {
		if err := o.observer.Run(ctx); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("observer: %w", err)
		}
	})

	wg.Go(func() {
		if err := o.tracker.Run(ctx, o.obsCh); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("tracker: %w", err)
		}
	})

	// Fan-out: route UpdateHeartbeat to heartbeatCh, all domain updates to updateEventCh.
	heartbeatCh := make(chan types.Update, cap(o.updateCh))
	wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-o.updateCh:
				if !ok {
					close(heartbeatCh)
					return
				}
				upd := update
				if upd.Domain == nil {
					logger.Warn("dropping update with nil domain", "type", upd.Type)
					continue
				}
				if upd.Type == types.UpdateHeartbeat {
					select {
					case heartbeatCh <- upd:
					case <-ctx.Done():
						return
					default:
						logger.Warn("dropping heartbeat update because heartbeat queue is full", "uuid", upd.Domain.UUID)
					}
				} else if o.updateEventCh != nil {
					select {
					case o.updateEventCh <- ctrlevent.TypedGenericEvent[*types.Update]{Object: &upd}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	})

	wg.Go(func() {
		if err := o.exporter.RunHeartbeats(ctx, heartbeatCh); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("exporter: %w", err)
		}
	})

	logger.Info("all components launched")

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		logger.Info("shutting down", "reason", ctx.Err())
	case err := <-errCh:
		logger.Error("component error", "err", err)
		return err
	}

	wg.Wait()
	logger.Info("shutdown complete")
	return ctx.Err()
}

func heartbeatTTL(cfg *config.Config) time.Duration {
	period := max(cfg.HeartbeatPeriod, 0)
	// Consumers use status.ttl as a validity window for Kubernetes-exposed data:
	// if now - status.observed exceeds this value, the heartbeat should be treated
	// as stale. The intuitive lower bound is pollInterval*heartbeatPeriod plus the
	// discardThreshold mute window. However, muting currently drops a poll instead
	// of delaying it, so a muted due poll can defer the next no-change heartbeat by
	// one full poll interval. Use pollInterval*(heartbeatPeriod+1) as that upper
	// bound; when heartbeatPeriod is 0, heartbeats are emitted every poll.
	return cfg.PollInterval * time.Duration(period+1)
}

// Tracker returns the state tracker for inspection (mainly for testing).
func (o *Operator) Tracker() *state.Tracker {
	return o.tracker
}

// Exporter returns the exporter for configuration (mainly for tracing).
func (o *Operator) Exporter() *exporter.Exporter {
	return o.exporter
}

// buildRestConfig returns a REST config from the given kubeconfig path,
// or falls back to in-cluster config if empty.
func buildRestConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	return ctrl.GetConfig()
}
