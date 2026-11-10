// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package exporter writes heartbeat CRDs to Kubernetes.
// Domain CRD writes are handled by DomainWriteController.
package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api/v1alpha1"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"

	"golang.org/x/time/rate"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var logger = logging.For("exporter")

// TraceCallback is called for trace events (category, name, phase, args).
type TraceCallback func(cat, name, phase string, args map[string]any)

// DomainKnownStore manages the UUID → UID mapping for heartbeat owner references.
type DomainKnownStore interface {
	SetKnownDomain(uuid, uid string)
	InvalidateDomain(uuid string)
}

// Exporter writes heartbeat CRDs to Kubernetes.
// Domain CRD writes are handled by DomainWriteController.
type Exporter struct {
	client   dynamic.Interface
	hostname string
	ttl      string

	heartbeatGVR schema.GroupVersionResource

	// Rate limiting
	heartbeatLimiter *rate.Limiter

	// Tracing
	traceCallback TraceCallback

	// knownDomains tracks UUIDs for which the domain CRD was successfully written,
	// mapping UUID -> domain UID. Used to set the owner reference on heartbeats
	// and skip heartbeat writes until the domain CRD exists.
	// All accesses are protected by knownDomainsMu.
	knownDomains   map[string]string
	knownDomainsMu sync.Mutex
}

// NewExporter creates a new Kubernetes exporter.
// If kubeconfig is empty, uses in-cluster config.
func NewExporter(kubeconfig, hostname string, heartbeatWriteRate float64) (*Exporter, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("create k8s config: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return NewExporterWithConfig(client, ExporterConfig{
		Hostname:           hostname,
		HeartbeatWriteRate: heartbeatWriteRate,
	}), nil
}

// ExporterConfig holds exporter configuration.
type ExporterConfig struct {
	Hostname           string
	HeartbeatTTL       time.Duration // consumer validity window for heartbeat CRDs
	HeartbeatWriteRate float64       // writes/sec for heartbeat CRDs (0 = unlimited)
}

// NewExporterWithClient creates an exporter with a provided client (for testing).
func NewExporterWithClient(client dynamic.Interface, hostname string) *Exporter {
	return NewExporterWithConfig(client, ExporterConfig{
		Hostname: hostname,
	})
}

// NewExporterWithConfig creates an exporter with full configuration.
func NewExporterWithConfig(client dynamic.Interface, cfg ExporterConfig) *Exporter {
	e := &Exporter{
		client:   client,
		hostname: cfg.Hostname,
		ttl:      cfg.HeartbeatTTL.String(),
		heartbeatGVR: schema.GroupVersionResource{
			Group:    crdmeta.Group,
			Version:  crdmeta.Version,
			Resource: crdmeta.LibvirtDomainHeartbeatResource(),
		},
		knownDomains: make(map[string]string),
	}

	if cfg.HeartbeatWriteRate > 0 {
		e.heartbeatLimiter = rate.NewLimiter(rate.Limit(cfg.HeartbeatWriteRate), 1)
	}

	return e
}

// RunHeartbeats processes UpdateHeartbeat events and writes heartbeat CRDs.
// This is a simple poll-push loop — no retry needed since the next poll is the natural retry.
func (e *Exporter) RunHeartbeats(ctx context.Context, heartbeatCh <-chan types.Update) error {
	logger.Info("started", "hostname", e.hostname)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update, ok := <-heartbeatCh:
			if !ok {
				return nil
			}
			if update.Domain == nil {
				logger.Warn("skipping heartbeat update with nil domain")
				continue
			}
			if err := e.createOrUpdateHeartbeat(ctx, update.Domain); err != nil {
				logger.Error("heartbeat write failed", "uuid", update.Domain.UUID, "err", err)
			}
		}
	}
}

// SetKnownDomain records the UID of a successfully written domain CRD.
// Called by DomainWriteController after a successful write.
func (e *Exporter) SetKnownDomain(uuid, uid string) {
	e.knownDomainsMu.Lock()
	defer e.knownDomainsMu.Unlock()
	e.knownDomains[uuid] = uid
}

// InvalidateDomain removes a UUID from the known domains cache.
// Called by the drift controller after externally recreating a domain CRD.
func (e *Exporter) InvalidateDomain(uuid string) {
	e.knownDomainsMu.Lock()
	defer e.knownDomainsMu.Unlock()
	delete(e.knownDomains, uuid)
}

func (e *Exporter) createOrUpdateHeartbeat(ctx context.Context, domain *types.DomainState) error {
	if e.heartbeatLimiter != nil {
		if err := e.heartbeatLimiter.Wait(ctx); err != nil {
			logger.Warn("heartbeat rate limit wait cancelled", "uuid", domain.UUID, "err", err)
			return fmt.Errorf("rate limit: %w", err)
		}
	}

	e.knownDomainsMu.Lock()
	uid, known := e.knownDomains[domain.UUID]
	e.knownDomainsMu.Unlock()

	if !known {
		// Domain CRD not yet written — skip until domain is established.
		logger.Debug("skipping heartbeat write, domain not yet known", "uuid", domain.UUID)
		e.trace("heartbeat", domain.UUID, "skip", map[string]any{"uuid": domain.UUID, "name": domain.Name})
		return nil
	}

	crd := v1alpha1.DomainToHeartbeat(domain, e.ttl)
	obj, err := toUnstructured(crd)
	if err != nil {
		return fmt.Errorf("convert to unstructured: %w", err)
	}

	ownerRef := metav1.OwnerReference{
		APIVersion: crdmeta.APIVersion(),
		Kind:       crdmeta.LibvirtDomainKind(),
		Name:       domain.Host + "-" + domain.UUID,
		UID:        k8stypes.UID(uid),
	}
	obj.SetOwnerReferences([]metav1.OwnerReference{ownerRef})

	name := crd.Name
	existing, err := e.client.Resource(e.heartbeatGVR).Get(ctx, name, metav1.GetOptions{})
	switch {
	case err != nil && !k8serrors.IsNotFound(err):
		return fmt.Errorf("get heartbeat CRD: %w", err)
	case err == nil:
		obj.SetResourceVersion(existing.GetResourceVersion())
		result, err := e.client.Resource(e.heartbeatGVR).Update(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update heartbeat CRD: %w", err)
		}
		obj.SetResourceVersion(result.GetResourceVersion())
		_, err = e.client.Resource(e.heartbeatGVR).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update heartbeat CRD status: %w", err)
		}
		e.trace("heartbeat", domain.UUID, "update", map[string]any{"uuid": domain.UUID, "name": domain.Name})
		return nil
	}

	result, err := e.client.Resource(e.heartbeatGVR).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create heartbeat CRD: %w", err)
	}
	obj.SetResourceVersion(result.GetResourceVersion())
	_, err = e.client.Resource(e.heartbeatGVR).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("create heartbeat CRD status: %w", err)
	}
	e.trace("heartbeat", domain.UUID, "create", map[string]any{"uuid": domain.UUID, "name": domain.Name})
	return nil
}

// SetTraceCallback sets the callback for trace events.
func (e *Exporter) SetTraceCallback(cb TraceCallback) {
	e.traceCallback = cb
}

func (e *Exporter) trace(cat, name, phase string, args map[string]any) {
	if e.traceCallback != nil {
		e.traceCallback(cat, name, phase, args)
	}
}

func toUnstructured(obj any) (*unstructured.Unstructured, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: result}, nil
}
