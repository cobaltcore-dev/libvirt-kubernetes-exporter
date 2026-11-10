// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package state manages the operator's internal domain state.
//
// It receives observations from libvirt (polling and events) and maintains
// a consistent view of all domains. When state changes, it emits updates
// to be exported to Kubernetes.
package state

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/domainxml"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var logger = logging.For("tracker")

// EventCallback is called when domain state changes (for SSE broadcasting).
type EventCallback func(eventType string, domain *types.DomainState)

// TraceCallback is called for trace events (category, name, phase, args).
type TraceCallback func(cat, name, phase string, args map[string]any)

// Tracker maintains the current state of all domains.
type Tracker struct {
	mu       sync.RWMutex
	domains  map[string]*types.DomainState // keyed by UUID
	hostname string
	version  uint64 // global version counter

	// seeded is set after the first successful libvirt poll observation is processed.
	// Events alone do not count — a poll guarantees a full view of all running VMs.
	seeded atomic.Bool

	// Event muting: track last event time per UUID
	lastEventTime    map[string]time.Time
	discardThreshold time.Duration

	// Heartbeat throttling (C2 feature)
	heartbeatPeriod int            // emit heartbeat every N polls (0 = always emit)
	pollCount       map[string]int // UUID -> polls since last heartbeat emit

	updateCh      chan<- types.Update
	eventCallback EventCallback
	traceCallback TraceCallback
}

// TrackerConfig holds Tracker configuration options.
type TrackerConfig struct {
	DiscardThreshold time.Duration
	HeartbeatPeriod  int // emit heartbeat every N polls (0 = always emit)
}

// NewTracker creates a new state tracker.
func NewTracker(hostname string, updateCh chan<- types.Update) *Tracker {
	return NewTrackerWithConfig(hostname, updateCh, TrackerConfig{})
}

// NewTrackerWithConfig creates a new state tracker with configuration.
func NewTrackerWithConfig(hostname string, updateCh chan<- types.Update, cfg TrackerConfig) *Tracker {
	return &Tracker{
		domains:          make(map[string]*types.DomainState),
		hostname:         hostname,
		updateCh:         updateCh,
		lastEventTime:    make(map[string]time.Time),
		discardThreshold: cfg.DiscardThreshold,
		heartbeatPeriod:  cfg.HeartbeatPeriod,
		pollCount:        make(map[string]int),
	}
}

// SetEventCallback sets a callback for domain events (used for SSE broadcasting).
func (t *Tracker) SetEventCallback(cb EventCallback) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eventCallback = cb
}

// SetTraceCallback sets a callback for trace events.
func (t *Tracker) SetTraceCallback(cb TraceCallback) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.traceCallback = cb
}

// notifyEvent calls the event callback if set.
func (t *Tracker) notifyEvent(eventType string, domain *types.DomainState) {
	if t.eventCallback != nil {
		t.eventCallback(eventType, domain)
	}
}

// trace emits a trace event if callback is set.
func (t *Tracker) trace(name string, args map[string]any) {
	if t.traceCallback != nil {
		t.traceCallback("tracker", name, "i", args)
	}
}

// Run processes observations from the channel until context is cancelled.
// This should be called from a single goroutine.
func (t *Tracker) Run(ctx context.Context, obsCh <-chan types.Observation) error {
	logger.Info("started", "hostname", t.hostname)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case obs := <-obsCh:
			t.processObservation(obs)
		}
	}
}

// processObservation updates internal state based on an observation.
func (t *Tracker) processObservation(obs types.Observation) {
	if obs.Source == types.SourcePoll {
		t.seeded.Store(true)
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	logger.Debug("received observation", "name", obs.Name, "uuid", obs.UUID, "state", obs.State, "source", obs.Source, "deleted", obs.IsDeleted)

	// IsDeleted observations are NEVER muted — poll is authority for existence
	if obs.IsDeleted {
		t.processDelete(obs)
		return
	}

	// Track event times for muting
	if obs.Source == types.SourceEvent {
		t.lastEventTime[obs.UUID] = obs.Observed
	}

	// Event muting: discard poll if recent event exists
	if t.shouldMute(obs) {
		logger.Debug("muting poll observation (recent event)", "uuid", obs.UUID)
		return
	}

	existing, exists := t.domains[obs.UUID]
	if !exists {
		t.processCreate(obs)
		return
	}

	t.processUpdate(existing, obs)
}

// shouldMute returns true if the observation should be discarded due to a recent event.
func (t *Tracker) shouldMute(obs types.Observation) bool {
	// Only mute poll observations
	if obs.Source != types.SourcePoll {
		return false
	}

	// No muting if threshold is zero
	if t.discardThreshold == 0 {
		return false
	}

	// Check if there was a recent event for this UUID
	lastEvent, exists := t.lastEventTime[obs.UUID]
	if !exists {
		return false
	}

	return obs.Observed.Sub(lastEvent) < t.discardThreshold
}

func (t *Tracker) processCreate(obs types.Observation) {
	t.version++

	domain := &types.DomainState{
		UUID:            obs.UUID,
		Name:            obs.Name,
		Host:            t.hostname,
		LibvirtDriver:   obs.LibvirtDriver,
		MaxMem:          obs.MaxMem,
		Memory:          obs.Memory,
		NrVirtCPU:       obs.NrVirtCPU,
		CPUTime:         obs.CPUTime,
		StateReasonCode: obs.StateReasonCode,
		Migration:       types.MigrationObservationToStatus(obs.MigrationInfo),
		FirstSeen:       obs.Observed,
		Version:         t.version,
	}

	// Set observed values
	domain.Status.Update(obs.State, obs.Queried, obs.Observed, obs.Source)
	if obs.XMLHash != "" {
		domain.XMLHash.Update(obs.XMLHash, obs.Queried, obs.Observed, obs.Source)
		domain.XML = obs.XML
		domain.NovaMetadata = domainxml.ParseNovaMetadata(obs.XML)
	}

	t.domains[obs.UUID] = domain

	domainCopy := domain.DeepCopy()
	t.emitUpdate(types.Update{
		Domain:    domainCopy,
		Type:      types.UpdateCreated,
		Timestamp: obs.Observed,
	})

	logger.Info("created domain", "name", obs.Name, "uuid", obs.UUID, "state", obs.State)
	t.notifyEvent("domain_created", domainCopy)
	t.trace("domain_created", map[string]any{
		"uuid": obs.UUID, "name": obs.Name, "state": obs.State.String(),
	})
}

func (t *Tracker) processUpdate(existing *types.DomainState, obs types.Observation) {
	// Handle VM reappearing during retention
	if existing.DeletedAt != nil {
		existing.DeletedAt = nil
		logger.Info("domain reappeared during retention", "name", existing.Name, "uuid", obs.UUID)
	}

	newMigration := types.MigrationObservationToStatus(obs.MigrationInfo)

	// Detect data changes (state, config)
	dataChanged := existing.Status.Value != obs.State ||
		existing.StateReasonCode != obs.StateReasonCode ||
		existing.Memory != obs.Memory ||
		existing.NrVirtCPU != obs.NrVirtCPU ||
		(obs.XMLHash != "" && existing.XMLHash.Value != obs.XMLHash) ||
		migrationChanged(existing.Migration, newMigration)

	// Always update timestamps and resource values
	existing.Status.Update(obs.State, obs.Queried, obs.Observed, obs.Source)
	if obs.LibvirtDriver != "" {
		existing.LibvirtDriver = obs.LibvirtDriver
	}
	if obs.XMLHash != "" {
		xmlChanged := existing.XMLHash.Value != obs.XMLHash
		existing.XMLHash.Update(obs.XMLHash, obs.Queried, obs.Observed, obs.Source)
		if xmlChanged {
			existing.XML = obs.XML
			existing.NovaMetadata = domainxml.ParseNovaMetadata(obs.XML)
		}
	}
	if obs.Source == types.SourcePoll {
		existing.MaxMem = obs.MaxMem
		existing.Memory = obs.Memory
		existing.NrVirtCPU = obs.NrVirtCPU
		existing.CPUTime = obs.CPUTime
	}
	existing.StateReasonCode = obs.StateReasonCode
	existing.Migration = newMigration

	// Bump version
	t.version++
	existing.Version = t.version

	domainCopy := existing.DeepCopy()
	switch {
	case dataChanged:
		// Data changed - emit immediately, reset heartbeat counter
		t.pollCount[obs.UUID] = 0
		t.emitUpdate(types.Update{
			Domain:    domainCopy,
			Type:      types.UpdateDataChanged,
			Timestamp: obs.Observed,
		})
		logger.Info("data changed", "name", obs.Name, "uuid", obs.UUID, "state", obs.State, "state_reason_code", obs.StateReasonCode, "migration", obs.MigrationInfo != nil)
		t.notifyEvent("domain_updated", domainCopy)
		t.trace("domain_updated", map[string]any{
			"uuid": obs.UUID, "name": obs.Name, "state": obs.State.String(),
		})
	case t.shouldEmitHeartbeat(obs.UUID):
		t.emitUpdate(types.Update{
			Domain:    domainCopy,
			Type:      types.UpdateHeartbeat,
			Timestamp: obs.Observed,
		})
		logger.Debug("heartbeat emitted", "name", obs.Name, "uuid", obs.UUID)
		t.trace("heartbeat", map[string]any{
			"uuid": obs.UUID, "name": obs.Name,
		})
	default:
		logger.Debug("no-change poll, heartbeat throttled", "name", obs.Name, "uuid", obs.UUID, "state", obs.State)
	}
}

// shouldEmitHeartbeat checks if a heartbeat should be emitted for this UUID.
// Implements poll count-based throttling with seeded spreading.
func (t *Tracker) shouldEmitHeartbeat(uuid string) bool {
	if t.heartbeatPeriod <= 0 {
		return true // no throttling
	}

	t.pollCount[uuid]++
	count := t.pollCount[uuid]

	// Seed based on UUID for spreading heartbeats across VMs
	seed := hashUUID(uuid) % t.heartbeatPeriod
	effectiveCount := (count + seed) % t.heartbeatPeriod

	return effectiveCount == 0
}

// hashUUID returns a deterministic hash of the UUID for spreading.
func hashUUID(uuid string) int {
	h := fnv.New32a()
	h.Write([]byte(uuid))
	return int(h.Sum32())
}

// migrationChanged returns true if any migration field changed.
func migrationChanged(existing, newStatus *types.MigrationStatus) bool {
	return !existing.Equal(newStatus)
}

func (t *Tracker) processDelete(obs types.Observation) {
	existing, exists := t.domains[obs.UUID]
	if !exists {
		return // already gone
	}

	// Already in gone state — no-op, retention controller handles deletion.
	if existing.DeletedAt != nil {
		return
	}

	// Transition to StateGone and set DeletedAt — retention controller will delete the CRD.
	now := obs.Observed
	existing.DeletedAt = &now
	existing.Status.Update(types.StateGone, obs.Queried, obs.Observed, obs.Source)
	t.version++
	existing.Version = t.version

	domainCopy := existing.DeepCopy()
	t.emitUpdate(types.Update{
		Domain:    domainCopy,
		Type:      types.UpdateDataChanged,
		Timestamp: obs.Observed,
	})

	logger.Info("domain marked as gone", "name", existing.Name, "uuid", obs.UUID)
	t.notifyEvent("domain_gone", domainCopy)
	t.trace("domain_gone", map[string]any{
		"uuid": obs.UUID, "name": existing.Name,
	})
}

func (t *Tracker) emitUpdate(update types.Update) {
	select {
	case t.updateCh <- update:
	default:
		logger.Warn("update channel full, dropping update", "uuid", update.Domain.UUID, "type", update.Type)
	}
}

// Ready returns true after the first successful libvirt poll has been processed.
// The drift controller uses this to avoid acting on an empty tracker at startup.
func (t *Tracker) Ready() bool {
	return t.seeded.Load()
}

// GetDomain retrieves a domain by UUID.
func (t *Tracker) GetDomain(uuid string) (*types.DomainState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	d, ok := t.domains[uuid]
	if !ok {
		return nil, false
	}
	return d.DeepCopy(), true
}

// ListDomains returns all tracked domains.
func (t *Tracker) ListDomains() []*types.DomainState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]*types.DomainState, 0, len(t.domains))
	for _, d := range t.domains {
		result = append(result, d.DeepCopy())
	}
	return result
}

// Count returns the number of tracked domains.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.domains)
}

// TrackedUUIDs returns all UUIDs currently tracked, including those in retention.
// Used by orphan cleanup to avoid deleting CRDs for VMs we're still tracking.
func (t *Tracker) TrackedUUIDs() map[string]struct{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]struct{}, len(t.domains))
	for uuid := range t.domains {
		result[uuid] = struct{}{}
	}
	return result
}
