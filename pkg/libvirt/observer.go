// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package libvirt provides the libvirt observation layer for the operator.
//
// It connects to libvirtd via a Unix socket and observes domain state
// through both polling and lifecycle events.
package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var logger = logging.For("observer")

// Observer watches libvirt for domain changes.
type Observer struct {
	socketPath   string
	libvirtURI   string
	hostname     string
	pollInterval time.Duration
	obsCh        chan<- types.Observation

	// For missing VM detection - tracks UUIDs seen in last successful poll
	lastSeenUUIDs map[string]struct{}
	lastSeenMu    sync.Mutex

	// Query by UUID on disappearance (B3 feature)
	queryByUUID bool

	// Staggered polling (C1 feature)
	pollRate    float64 // target VMs/second (0 = no staggering)
	maxPollRate float64 // rate limit for many VMs

	// For graceful shutdown
	wg sync.WaitGroup
}

// NewObserver creates a new libvirt observer.
func NewObserver(socketPath, hostname string, obsCh chan<- types.Observation) *Observer {
	return &Observer{
		socketPath:    socketPath,
		hostname:      hostname,
		pollInterval:  10 * time.Second,
		obsCh:         obsCh,
		lastSeenUUIDs: make(map[string]struct{}),
	}
}

// SetPollInterval configures the polling interval.
func (o *Observer) SetPollInterval(d time.Duration) {
	o.pollInterval = d
}

// SetQueryByUUID enables/disables direct UUID lookup on VM disappearance.
func (o *Observer) SetQueryByUUID(enabled bool) {
	o.queryByUUID = enabled
}

// SetLibvirtURI sets the libvirt connection URI (e.g. "qemu:///system").
// When set, ConnectToURI is used instead of Connect.
func (o *Observer) SetLibvirtURI(uri string) {
	o.libvirtURI = uri
}

// SetStaggeredPolling configures staggered per-VM polling.
// pollRate is the target VMs/second (0 = no staggering).
// maxPollRate is the rate limit when many VMs would exceed poll interval.
func (o *Observer) SetStaggeredPolling(pollRate, maxPollRate float64) {
	o.pollRate = pollRate
	o.maxPollRate = maxPollRate
}

// Run starts both the poller and event listener goroutines.
// It blocks until the context is cancelled.
func (o *Observer) Run(ctx context.Context) error {
	o.wg.Add(2)

	go func() {
		defer o.wg.Done()
		o.runPoller(ctx)
	}()

	go func() {
		defer o.wg.Done()
		o.runEventListener(ctx)
	}()

	<-ctx.Done()
	o.wg.Wait()
	return ctx.Err()
}

// connect establishes a connection to libvirt.
func (o *Observer) connect() (*libvirt.Libvirt, error) {
	l := libvirt.NewWithDialer(dialers.NewLocal(dialers.WithSocket(o.socketPath)))
	logger.Debug("connecting to libvirt", "socket", o.socketPath, "uri", o.libvirtURI)
	errCh := make(chan error, 1)
	go func() { errCh <- l.ConnectToURI(libvirt.ConnectURI(o.libvirtURI)) }()
	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	logger.Debug("connected to libvirt", "socket", o.socketPath)
	return l, nil
}

// runPoller polls libvirt for domain state at regular intervals.
func (o *Observer) runPoller(ctx context.Context) {
	// Initial poll immediately
	if err := o.poll(ctx); err != nil {
		logger.Error("initial poll error", "err", err)
	}

	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	var consecutiveErrors int
	var lastError string

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.poll(ctx); err != nil {
				consecutiveErrors++
				errStr := err.Error()
				// Only log if error message changed or every 10th consecutive error
				if errStr != lastError || consecutiveErrors%10 == 1 {
					if consecutiveErrors > 1 {
						logger.Warn("poll error", "count", consecutiveErrors, "err", err)
					} else {
						logger.Warn("poll error", "err", err)
					}
				}
				lastError = errStr
			} else {
				if consecutiveErrors > 0 {
					logger.Info("connection restored", "after_errors", consecutiveErrors)
				}
				consecutiveErrors = 0
				lastError = ""
			}
		}
	}
}

func (o *Observer) poll(ctx context.Context) error {
	logger.Debug("poll started")
	queried := time.Now()

	l, err := o.connect()
	if err != nil {
		return err
	}
	defer func() {
		if err := l.Disconnect(); err != nil {
			logger.Warn("disconnect error", "err", err)
		}
	}()

	// List all domains (instant - unavoidable)
	domains, _, err := l.ConnectListAllDomains(1, 0) // needResults=1, flags=0 (all states)
	if err != nil {
		return fmt.Errorf("list domains: %w", err)
	}

	logger.Debug("poll found domains", "count", len(domains))

	// Calculate stagger delay for C1 feature
	staggerDelay := o.calculateStaggerDelay(len(domains))

	// Build current seen set
	currentSeen := make(map[string]struct{}, len(domains))

	for i, dom := range domains {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Apply stagger delay between VMs (except for first one)
		if staggerDelay > 0 && i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(staggerDelay):
			}
		}

		obs, err := o.observeDomain(l, dom, queried, types.SourcePoll)
		if err != nil {
			logger.Error("observe domain error", "name", dom.Name, "err", err)
			continue
		}

		currentSeen[obs.UUID] = struct{}{}
		o.emit(ctx, obs)
	}

	// Detect missing VMs - poll is authoritative for existence
	o.detectMissing(ctx, currentSeen, queried)

	logger.Debug("poll complete", "domains", len(currentSeen), "duration", time.Since(queried))
	return nil
}

// calculateStaggerDelay calculates the delay between per-VM observations.
func (o *Observer) calculateStaggerDelay(vmCount int) time.Duration {
	if o.pollRate <= 0 || vmCount <= 1 {
		return 0 // no staggering
	}

	// Calculate effective rate
	effectiveRate := o.pollRate

	// Increase rate if too many VMs to fit in poll interval
	requiredRate := float64(vmCount) / o.pollInterval.Seconds()
	if requiredRate > effectiveRate {
		effectiveRate = requiredRate
		if o.maxPollRate > 0 && effectiveRate > o.maxPollRate {
			effectiveRate = o.maxPollRate
		}
	}

	// Delay = 1/rate seconds per VM
	return time.Duration(float64(time.Second) / effectiveRate)
}

// detectMissing emits IsDeleted observations for VMs that disappeared.
// Uses direct UUID lookup to verify VM is truly gone (not in transient state).
func (o *Observer) detectMissing(ctx context.Context, currentSeen map[string]struct{}, queried time.Time) {
	o.lastSeenMu.Lock()
	defer o.lastSeenMu.Unlock()

	now := time.Now()

	// Find VMs that were in lastSeenUUIDs but not in currentSeen
	for uuid := range o.lastSeenUUIDs {
		if _, exists := currentSeen[uuid]; !exists {
			switch {
			case o.queryByUUID && o.verifyMissing(uuid):
				o.emit(ctx, types.Observation{
					UUID:      uuid,
					IsDeleted: true,
					Queried:   queried,
					Observed:  now,
					Source:    types.SourcePoll,
				})
				logger.Info("VM no longer present (verified), emitting IsDeleted", "uuid", uuid)
			case !o.queryByUUID:
				o.emit(ctx, types.Observation{
					UUID:      uuid,
					IsDeleted: true,
					Queried:   queried,
					Observed:  now,
					Source:    types.SourcePoll,
				})
				logger.Info("VM no longer present, emitting IsDeleted", "uuid", uuid)
			default:
				logger.Debug("VM not in list but direct query succeeded (transient state)", "uuid", uuid)
			}
		}
	}

	// Update lastSeenUUIDs for next poll
	o.lastSeenUUIDs = currentSeen
}

// verifyMissing checks if a VM is truly gone by direct UUID lookup.
// Returns true if the VM is confirmed missing.
func (o *Observer) verifyMissing(uuid string) bool {
	l, err := o.connect()
	if err != nil {
		logger.Error("failed to connect for UUID verification", "err", err)
		return true // assume gone if we can't connect
	}
	defer func() {
		if err := l.Disconnect(); err != nil {
			logger.Warn("disconnect error", "err", err)
		}
	}()

	uuidBytes, err := parseUUID(uuid)
	if err != nil {
		logger.Error("invalid UUID format for verification", "uuid", uuid)
		return true
	}

	_, err = l.DomainLookupByUUID(uuidBytes)
	return err != nil // true if lookup failed (VM is gone)
}

func parseUUID(s string) (libvirt.UUID, error) {
	var uuid libvirt.UUID
	// Parse UUID string like "11111111-1111-1111-1111-111111111111"
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return uuid, fmt.Errorf("invalid UUID length: %d", len(s))
	}
	for i := range 16 {
		var b byte
		_, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &b)
		if err != nil {
			return uuid, err
		}
		uuid[i] = b
	}
	return uuid, nil
}

// observeDomain gathers full information about a domain.
func (o *Observer) observeDomain(l *libvirt.Libvirt, dom libvirt.Domain, queried time.Time, source types.ObservationSource) (types.Observation, error) {
	// Get domain info
	state, maxMem, mem, nrVCPU, cpuTime, err := l.DomainGetInfo(dom)
	if err != nil {
		return types.Observation{}, fmt.Errorf("DomainGetInfo: %w", err)
	}

	stateReason := o.fetchStateReason(l, dom)
	migrationInfo := o.fetchMigrationInfo(l, dom)

	// Get XML and compute hash (for change detection) - optional
	var xmlHash, xml string
	xml, err = l.DomainGetXMLDesc(dom, 0)
	if err == nil {
		xmlHash = hashXML(xml)
	}
	// If XML fetch fails, continue without hash (e.g., fake-libvirt may not support it)

	observed := time.Now()

	logger.Debug("observed domain", "name", dom.Name, "uuid", uuidToString(dom.UUID), "state", types.LibvirtState(state))

	return types.Observation{
		UUID:            uuidToString(dom.UUID),
		Name:            dom.Name,
		LibvirtDriver:   o.libvirtDriver(),
		State:           types.LibvirtState(state),
		StateReasonCode: stateReason,
		MaxMem:          maxMem,
		Memory:          mem,
		NrVirtCPU:       nrVCPU,
		CPUTime:         cpuTime,
		XMLHash:         xmlHash,
		XML:             xml,
		Queried:         queried,
		Observed:        observed,
		Source:          source,
		MigrationInfo:   migrationInfo,
	}, nil
}

// runEventListener listens for lifecycle events from libvirt.
// It automatically reconnects on disconnect.
func (o *Observer) runEventListener(ctx context.Context) {
	for {
		err := o.listenEvents(ctx)
		if ctx.Err() != nil {
			return
		}
		logger.Warn("event listener error, reconnecting in 5s", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (o *Observer) listenEvents(ctx context.Context) error {
	l, err := o.connect()
	if err != nil {
		return err
	}
	defer func() {
		if err := l.Disconnect(); err != nil {
			logger.Warn("disconnect error", "err", err)
		}
	}()

	events, err := l.LifecycleEvents(ctx)
	if err != nil {
		return fmt.Errorf("register events: %w", err)
	}

	logger.Info("listening for lifecycle events", "socket", o.socketPath)

	for event := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		obs := o.eventToObservation(l, event)
		logger.Debug("received lifecycle event", "name", obs.Name, "uuid", obs.UUID, "state", obs.State, "deleted", obs.IsDeleted)
		o.emit(ctx, obs)
	}

	return nil
}

func (o *Observer) eventToObservation(l *libvirt.Libvirt, event libvirt.DomainEventLifecycleMsg) types.Observation {
	now := time.Now()
	obs := types.Observation{
		UUID:          uuidToString(event.Dom.UUID),
		Name:          event.Dom.Name,
		LibvirtDriver: o.libvirtDriver(),
		Source:        types.SourceEvent,
		Queried:       now,
		Observed:      now,
	}

	// Map lifecycle event to state using go-libvirt typed constants.
	switch libvirt.DomainEventType(event.Event) {
	case libvirt.DomainEventDefined:
		obs.State = types.StateShutoff
	case libvirt.DomainEventUndefined:
		obs.IsDeleted = true
	case libvirt.DomainEventStarted:
		obs.State = types.StateRunning
	case libvirt.DomainEventSuspended:
		obs.State = types.StatePaused
	case libvirt.DomainEventResumed:
		obs.State = types.StateRunning
	case libvirt.DomainEventStopped:
		obs.State = types.StateShutoff
	case libvirt.DomainEventShutdown:
		obs.State = types.StateShutdown
	case libvirt.DomainEventPmsuspended:
		obs.State = types.StatePMSuspended
	case libvirt.DomainEventCrashed:
		obs.State = types.StateCrashed
	}

	if !obs.IsDeleted {
		obs.StateReasonCode = o.fetchStateReason(l, event.Dom)
		obs.MigrationInfo = o.fetchMigrationInfo(l, event.Dom)
	}

	return obs
}

// fetchStateReason calls DomainGetState and returns the reason code, or 0 on error.
func (o *Observer) fetchStateReason(l *libvirt.Libvirt, dom libvirt.Domain) int32 {
	_, reason, err := l.DomainGetState(dom, 0)
	if err != nil {
		logger.Debug("DomainGetState failed, reason unknown", "name", dom.Name, "err", err)
		return 0
	}
	return reason
}

// fetchMigrationInfo calls DomainGetJobStats unconditionally and returns a
// MigrationObservation when a job is active (jobType != none), or nil otherwise.
func (o *Observer) fetchMigrationInfo(l *libvirt.Libvirt, dom libvirt.Domain) *types.MigrationObservation {
	jobType, params, err := l.DomainGetJobStats(dom, 0)
	if err != nil {
		logger.Warn("DomainGetJobStats failed", "name", dom.Name, "err", err)
		return nil
	}
	if libvirt.DomainJobType(jobType) == libvirt.DomainJobNone {
		return nil
	}

	obs := &types.MigrationObservation{JobType: libvirt.DomainJobType(jobType)}
	for _, p := range params {
		if p.Field == libvirt.DomainJobOperationStr {
			setJobOperation(&obs.Operation, p.Value.I)
		}
	}
	return obs
}

func setJobOperation(dst *libvirt.DomainJobOperation, v any) {
	if i, ok := v.(int32); ok {
		*dst = libvirt.DomainJobOperation(i)
	}
}

func (o *Observer) libvirtDriver() string {
	if idx := strings.Index(o.libvirtURI, ":"); idx > 0 {
		return strings.ToUpper(o.libvirtURI[:idx])
	}
	return "UNKNOWN"
}

func (o *Observer) emit(ctx context.Context, obs types.Observation) {
	select {
	case o.obsCh <- obs:
	case <-ctx.Done():
	default:
		logger.Warn("observation channel full, dropping observation", "uuid", obs.UUID)
	}
}

func uuidToString(uuid libvirt.UUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

func hashXML(xml string) string {
	h := sha256.Sum256([]byte(xml))
	return hex.EncodeToString(h[:8]) // first 8 bytes = 16 hex chars
}
