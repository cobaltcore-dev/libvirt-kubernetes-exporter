// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"sync"
)

const (
	DomainNostate     = 0
	DomainRunning     = 1
	DomainBlocked     = 2
	DomainPaused      = 3
	DomainShutdown    = 4
	DomainShutoff     = 5
	DomainCrashed     = 6
	DomainPmsuspended = 7
)

type Domain struct {
	UUID        [16]byte
	Name        string
	ID          int32
	State       uint8
	StateReason int32
	MaxMem      uint64
	Memory      uint64
	NrVirtCPU   uint16
	CPUTime     uint64
	// Job stats from DomainGetJobStats; JobType=0 means no active job
	JobType      int32
	JobOperation int32
}

// LifecycleEvent represents a domain lifecycle event.
type LifecycleEvent struct {
	Domain *Domain
	Event  int32 // event type (Defined, Undefined, Started, Stopped, etc.)
	Detail int32 // detail reason
}

// EventSubscription represents an active event callback subscription.
type EventSubscription struct {
	CallbackID int32
	EventType  int32 // which event type this subscription is for
	EventCh    chan<- LifecycleEvent
}

type FakeState struct {
	mu      sync.RWMutex
	domains map[string]*Domain // keyed by UUID string

	subsMu         sync.RWMutex
	subscriptions  map[int32]*EventSubscription // keyed by callback ID
	nextCallbackID int32

	// Visibility control for testing transient states
	hiddenFromList map[string]struct{} // UUIDs hidden from ListDomains but queryable
}

func New() *FakeState {
	s := &FakeState{
		domains:        make(map[string]*Domain),
		subscriptions:  make(map[int32]*EventSubscription),
		nextCallbackID: 1000, // start at 1000 to make IDs recognizable
		hiddenFromList: make(map[string]struct{}),
	}
	s.loadTestDomains()
	return s
}

// NewEmpty creates an empty FakeState without any test domains.
func NewEmpty() *FakeState {
	return &FakeState{
		domains:        make(map[string]*Domain),
		subscriptions:  make(map[int32]*EventSubscription),
		nextCallbackID: 1000,
		hiddenFromList: make(map[string]struct{}),
	}
}

func (s *FakeState) ListDomains() []*Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Domain, 0, len(s.domains))
	for key, d := range s.domains {
		if _, hidden := s.hiddenFromList[key]; !hidden {
			result = append(result, d)
		}
	}
	return result
}

// HideFromList hides a domain from ListDomains but keeps it queryable via GetDomain.
// Used for testing transient VM states.
func (s *FakeState) HideFromList(uuid [16]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hiddenFromList[uuidKey(uuid)] = struct{}{}
}

// ShowInList restores a domain's visibility in ListDomains.
func (s *FakeState) ShowInList(uuid [16]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hiddenFromList, uuidKey(uuid))
}

// IsHiddenFromList returns true if the domain is hidden from ListDomains.
func (s *FakeState) IsHiddenFromList(uuid [16]byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, hidden := s.hiddenFromList[uuidKey(uuid)]
	return hidden
}

func (s *FakeState) GetDomain(uuid [16]byte) (*Domain, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.domains[uuidKey(uuid)]
	return d, ok
}

func (s *FakeState) GetDomainByName(name string) *Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.domains {
		if d.Name == name {
			return d
		}
	}
	return nil
}

func (s *FakeState) AddDomain(d *Domain) error {
	s.mu.Lock()
	key := uuidKey(d.UUID)
	if _, exists := s.domains[key]; exists {
		s.mu.Unlock()
		return fmt.Errorf("domain with UUID %x already exists", d.UUID)
	}
	for _, existing := range s.domains {
		if existing.Name == d.Name {
			s.mu.Unlock()
			return fmt.Errorf("domain with name %q already exists", d.Name)
		}
	}
	s.domains[key] = d
	s.mu.Unlock()

	// Publish Defined event
	s.publishEvent(LifecycleEvent{
		Domain: d,
		Event:  DomainEventDefined,
		Detail: DetailUnknown,
	})

	return nil
}

func (s *FakeState) RemoveDomain(uuid [16]byte) error {
	s.mu.Lock()
	key := uuidKey(uuid)
	d, exists := s.domains[key]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("domain not found: %x", uuid)
	}
	delete(s.domains, key)
	s.mu.Unlock()

	// Publish Undefined event
	s.publishEvent(LifecycleEvent{
		Domain: d,
		Event:  DomainEventUndefined,
		Detail: DetailUnknown,
	})

	return nil
}

func (s *FakeState) SetDomainState(uuid [16]byte, newState uint8) error {
	return s.SetDomainStateWithReason(uuid, newState, 0)
}

// SetDomainStateWithReason sets both state and reason code on a domain.
func (s *FakeState) SetDomainStateWithReason(uuid [16]byte, newState uint8, reason int32) error {
	s.mu.Lock()
	d, ok := s.domains[uuidKey(uuid)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("domain not found: %x", uuid)
	}

	oldState := d.State
	d.State = newState
	d.StateReason = reason
	if newState == DomainRunning || newState == DomainPaused || newState == DomainBlocked {
		if d.ID < 0 {
			d.ID = 1
		}
	} else {
		d.ID = -1
	}
	s.mu.Unlock()

	if oldState != newState {
		event := stateChangeToEvent(oldState, newState)
		if event >= 0 {
			s.publishEvent(LifecycleEvent{
				Domain: d,
				Event:  event,
				Detail: DetailUnknown,
			})
		}
	}
	return nil
}

// SetDomainJobStats sets migration job statistics on a domain.
// Set jobType=0 to clear (no active job).
func (s *FakeState) SetDomainJobStats(uuid [16]byte, d *Domain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.domains[uuidKey(uuid)]
	if !ok {
		return fmt.Errorf("domain not found: %x", uuid)
	}
	existing.JobType = d.JobType
	existing.JobOperation = d.JobOperation
	return nil
}

// stateChangeToEvent maps domain state transitions to lifecycle events.
func stateChangeToEvent(oldState, newState uint8) int32 {
	switch {
	case newState == DomainRunning && (oldState == DomainShutoff || oldState == DomainNostate):
		return DomainEventStarted
	case newState == DomainShutoff && oldState == DomainRunning:
		return DomainEventStopped
	case newState == DomainPaused && oldState == DomainRunning:
		return DomainEventSuspended
	case newState == DomainRunning && oldState == DomainPaused:
		return DomainEventResumed
	case newState == DomainShutdown:
		return DomainEventShutdown
	case newState == DomainCrashed:
		return DomainEventCrashed
	default:
		return -1 // no event for this transition
	}
}

func uuidKey(uuid [16]byte) string {
	return string(uuid[:])
}

// RegisterEventCallback registers a callback for domain events.
// Returns the callback ID which can be used to deregister.
func (s *FakeState) RegisterEventCallback(eventType int32, ch chan<- LifecycleEvent) int32 {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	callbackID := s.nextCallbackID
	s.nextCallbackID++

	s.subscriptions[callbackID] = &EventSubscription{
		CallbackID: callbackID,
		EventType:  eventType,
		EventCh:    ch,
	}

	return callbackID
}

// DeregisterEventCallback removes an event callback registration.
func (s *FakeState) DeregisterEventCallback(callbackID int32) error {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	if _, exists := s.subscriptions[callbackID]; !exists {
		return fmt.Errorf("callback %d not found", callbackID)
	}
	delete(s.subscriptions, callbackID)
	return nil
}

// PublishEvent sends an event to all matching subscribers.
// This is the public API for test harnesses to inject events.
func (s *FakeState) PublishEvent(event LifecycleEvent) {
	s.publishEvent(event)
}

// publishEvent sends an event to all matching subscribers.
func (s *FakeState) publishEvent(event LifecycleEvent) {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()

	for _, sub := range s.subscriptions {
		// Event type 0 (lifecycle) matches all lifecycle events
		if sub.EventType == 0 {
			select {
			case sub.EventCh <- event:
			default:
				// non-blocking send, drop if channel full
			}
		}
	}
}

// Lifecycle event types (matching libvirt's VIR_DOMAIN_EVENT_*)
const (
	DomainEventDefined     = 0
	DomainEventUndefined   = 1
	DomainEventStarted     = 2
	DomainEventSuspended   = 3
	DomainEventResumed     = 4
	DomainEventStopped     = 5
	DomainEventShutdown    = 6
	DomainEventPmsuspended = 7
	DomainEventCrashed     = 8
)

// Lifecycle event detail reasons
const (
	DetailUnknown      = 0
	DetailBooted       = 1
	DetailMigrated     = 2
	DetailRestored     = 3
	DetailFromSnapshot = 4
)

func (s *FakeState) loadTestDomains() {
	testDomains := []*Domain{
		{
			UUID:        [16]byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11},
			Name:        "test-vm-running",
			ID:          1,
			State:       DomainRunning,
			StateReason: 1,       // DomainRunningBooted
			MaxMem:      2097152, // 2 GB in KB
			Memory:      1048576, // 1 GB in KB
			NrVirtCPU:   2,
			CPUTime:     123456789,
		},
		{
			UUID:        [16]byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22},
			Name:        "test-vm-shutoff",
			ID:          -1,
			State:       DomainShutoff,
			StateReason: 1, // DomainShutoffShutdown
			MaxMem:      1048576,
			Memory:      0,
			NrVirtCPU:   1,
			CPUTime:     0,
		},
		{
			UUID:        [16]byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33},
			Name:        "test-vm-paused",
			ID:          2,
			State:       DomainPaused,
			StateReason: 6, // DomainPausedWatchdog
			MaxMem:      4194304,
			Memory:      4194304,
			NrVirtCPU:   4,
			CPUTime:     987654321,
		},
	}
	for _, d := range testDomains {
		s.domains[uuidKey(d.UUID)] = d
	}
}
