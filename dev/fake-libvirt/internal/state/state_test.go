// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"
	"time"
)

func testDomain(name string, uuid [16]byte, state uint8) *Domain {
	return &Domain{
		UUID:      uuid,
		Name:      name,
		ID:        1,
		State:     state,
		MaxMem:    1048576,
		Memory:    524288,
		NrVirtCPU: 2,
		CPUTime:   0,
	}
}

func TestAddDomain(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}

	d := testDomain("vm-a", [16]byte{0x01}, DomainRunning)
	if err := s.AddDomain(d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := s.GetDomain(d.UUID)
	if !ok {
		t.Fatal("domain not found after add")
	}
	if got.Name != "vm-a" {
		t.Errorf("expected name vm-a, got %q", got.Name)
	}
}

func TestAddDomain_DuplicateUUID(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	uuid := [16]byte{0x02}

	s.AddDomain(testDomain("vm-a", uuid, DomainRunning))
	err := s.AddDomain(testDomain("vm-b", uuid, DomainRunning))
	if err == nil {
		t.Fatal("expected error for duplicate UUID")
	}
}

func TestAddDomain_DuplicateName(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}

	s.AddDomain(testDomain("vm-a", [16]byte{0x03}, DomainRunning))
	err := s.AddDomain(testDomain("vm-a", [16]byte{0x04}, DomainRunning))
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestRemoveDomain(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	d := testDomain("vm-a", [16]byte{0x05}, DomainRunning)
	s.AddDomain(d)

	if err := s.RemoveDomain(d.UUID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := s.GetDomain(d.UUID); ok {
		t.Fatal("domain still present after remove")
	}
}

func TestRemoveDomain_NotFound(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	err := s.RemoveDomain([16]byte{0xAA})
	if err == nil {
		t.Fatal("expected error for unknown UUID")
	}
}

func TestSetDomainState(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	d := testDomain("vm-a", [16]byte{0x06}, DomainRunning)
	s.AddDomain(d)

	if err := s.SetDomainState(d.UUID, DomainShutoff); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := s.GetDomain(d.UUID)
	if got.State != DomainShutoff {
		t.Errorf("expected state shutoff, got %d", got.State)
	}
	if got.ID != -1 {
		t.Errorf("expected ID -1 for inactive domain, got %d", got.ID)
	}
}

func TestSetDomainState_ActiveSetsID(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	d := testDomain("vm-a", [16]byte{0x07}, DomainShutoff)
	d.ID = -1
	s.AddDomain(d)

	if err := s.SetDomainState(d.UUID, DomainRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := s.GetDomain(d.UUID)
	if got.State != DomainRunning {
		t.Errorf("expected state running, got %d", got.State)
	}
	if got.ID < 0 {
		t.Errorf("expected positive ID for active domain, got %d", got.ID)
	}
}

func TestSetDomainState_NotFound(t *testing.T) {
	s := &FakeState{domains: make(map[string]*Domain)}
	err := s.SetDomainState([16]byte{0xBB}, DomainRunning)
	if err == nil {
		t.Fatal("expected error for unknown UUID")
	}
}

func newStateWithEvents() *FakeState {
	return &FakeState{
		domains:        make(map[string]*Domain),
		subscriptions:  make(map[int32]*EventSubscription),
		nextCallbackID: 1000,
	}
}

func TestEventSubscription(t *testing.T) {
	s := newStateWithEvents()
	ch := make(chan LifecycleEvent, 10)

	cbID := s.RegisterEventCallback(0, ch) // 0 = lifecycle events
	if cbID < 1000 {
		t.Errorf("expected callback ID >= 1000, got %d", cbID)
	}

	// Add a domain - should trigger Defined event
	d := testDomain("vm-event", [16]byte{0xE1}, DomainShutoff)
	d.ID = -1
	s.AddDomain(d)

	select {
	case ev := <-ch:
		if ev.Event != DomainEventDefined {
			t.Errorf("expected Defined event, got %d", ev.Event)
		}
		if ev.Domain.Name != "vm-event" {
			t.Errorf("expected domain name 'vm-event', got %q", ev.Domain.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Defined event")
	}

	// Start the domain - should trigger Started event
	s.SetDomainState(d.UUID, DomainRunning)

	select {
	case ev := <-ch:
		if ev.Event != DomainEventStarted {
			t.Errorf("expected Started event, got %d", ev.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Started event")
	}

	// Stop the domain - should trigger Stopped event
	s.SetDomainState(d.UUID, DomainShutoff)

	select {
	case ev := <-ch:
		if ev.Event != DomainEventStopped {
			t.Errorf("expected Stopped event, got %d", ev.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Stopped event")
	}

	// Remove domain - should trigger Undefined event
	s.RemoveDomain(d.UUID)

	select {
	case ev := <-ch:
		if ev.Event != DomainEventUndefined {
			t.Errorf("expected Undefined event, got %d", ev.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Undefined event")
	}
}

func TestEventDeregistration(t *testing.T) {
	s := newStateWithEvents()
	ch := make(chan LifecycleEvent, 10)

	cbID := s.RegisterEventCallback(0, ch)

	// Deregister
	if err := s.DeregisterEventCallback(cbID); err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	// Now add domain - should NOT receive event (channel should remain empty)
	d := testDomain("vm-dereg", [16]byte{0xE2}, DomainShutoff)
	d.ID = -1
	s.AddDomain(d)

	select {
	case ev := <-ch:
		t.Errorf("received unexpected event after deregistration: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// Good, no event received
	}
}

func TestEventDeregistration_NotFound(t *testing.T) {
	s := newStateWithEvents()
	err := s.DeregisterEventCallback(9999)
	if err == nil {
		t.Fatal("expected error for unknown callback ID")
	}
}
