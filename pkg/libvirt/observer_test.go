// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package libvirt

import (
	"context"
	"testing"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

func TestDetectMissing(t *testing.T) {
	obsCh := make(chan types.Observation, 10)
	observer := NewObserver("/nonexistent", "test-host", obsCh)

	// Simulate first poll saw two VMs
	observer.lastSeenUUIDs = map[string]struct{}{
		"uuid-001": {},
		"uuid-002": {},
	}

	// Second poll only sees uuid-001
	currentSeen := map[string]struct{}{
		"uuid-001": {},
	}

	ctx := context.Background()
	queried := time.Now()
	observer.detectMissing(ctx, currentSeen, queried)

	// Should emit IsDeleted for uuid-002
	select {
	case obs := <-obsCh:
		if obs.UUID != "uuid-002" {
			t.Errorf("expected UUID uuid-002, got %s", obs.UUID)
		}
		if !obs.IsDeleted {
			t.Error("expected IsDeleted to be true")
		}
		if obs.Source != types.SourcePoll {
			t.Errorf("expected SourcePoll, got %s", obs.Source)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected IsDeleted observation not received")
	}

	// Should not emit for uuid-001
	select {
	case obs := <-obsCh:
		t.Errorf("unexpected observation for %s", obs.UUID)
	case <-time.After(10 * time.Millisecond):
		// expected - no more observations
	}

	// Verify lastSeenUUIDs was updated
	if len(observer.lastSeenUUIDs) != 1 {
		t.Errorf("expected lastSeenUUIDs to have 1 entry, got %d", len(observer.lastSeenUUIDs))
	}
	if _, exists := observer.lastSeenUUIDs["uuid-001"]; !exists {
		t.Error("expected uuid-001 to be in lastSeenUUIDs")
	}
}

func TestDetectMissingMultiple(t *testing.T) {
	obsCh := make(chan types.Observation, 10)
	observer := NewObserver("/nonexistent", "test-host", obsCh)

	// First poll saw three VMs
	observer.lastSeenUUIDs = map[string]struct{}{
		"uuid-001": {},
		"uuid-002": {},
		"uuid-003": {},
	}

	// Second poll sees only uuid-001
	currentSeen := map[string]struct{}{
		"uuid-001": {},
	}

	ctx := context.Background()
	observer.detectMissing(ctx, currentSeen, time.Now())

	// Should emit IsDeleted for uuid-002 and uuid-003
	deleted := make(map[string]bool)
	for range 2 {
		select {
		case obs := <-obsCh:
			if !obs.IsDeleted {
				t.Error("expected IsDeleted to be true")
			}
			deleted[obs.UUID] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected observation not received")
		}
	}

	if !deleted["uuid-002"] {
		t.Error("expected uuid-002 to be deleted")
	}
	if !deleted["uuid-003"] {
		t.Error("expected uuid-003 to be deleted")
	}
}

func TestDetectMissingFirstPoll(t *testing.T) {
	obsCh := make(chan types.Observation, 10)
	observer := NewObserver("/nonexistent", "test-host", obsCh)

	// First poll - lastSeenUUIDs is empty
	currentSeen := map[string]struct{}{
		"uuid-001": {},
		"uuid-002": {},
	}

	ctx := context.Background()
	observer.detectMissing(ctx, currentSeen, time.Now())

	// Should not emit any IsDeleted (nothing was missing)
	select {
	case obs := <-obsCh:
		t.Errorf("unexpected observation for %s on first poll", obs.UUID)
	case <-time.After(10 * time.Millisecond):
		// expected - no observations
	}

	// Verify lastSeenUUIDs was updated
	if len(observer.lastSeenUUIDs) != 2 {
		t.Errorf("expected lastSeenUUIDs to have 2 entries, got %d", len(observer.lastSeenUUIDs))
	}
}

func TestDetectMissingVMReappears(t *testing.T) {
	obsCh := make(chan types.Observation, 10)
	observer := NewObserver("/nonexistent", "test-host", obsCh)

	ctx := context.Background()

	// Poll 1: sees uuid-001, uuid-002
	observer.lastSeenUUIDs = map[string]struct{}{}
	currentSeen1 := map[string]struct{}{"uuid-001": {}, "uuid-002": {}}
	observer.detectMissing(ctx, currentSeen1, time.Now())
	// drain any observations (none expected on first poll)
	drainCh(obsCh)

	// Poll 2: uuid-002 disappears
	currentSeen2 := map[string]struct{}{"uuid-001": {}}
	observer.detectMissing(ctx, currentSeen2, time.Now())

	// Should emit IsDeleted for uuid-002
	select {
	case obs := <-obsCh:
		if obs.UUID != "uuid-002" || !obs.IsDeleted {
			t.Errorf("expected IsDeleted for uuid-002, got %s deleted=%v", obs.UUID, obs.IsDeleted)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected IsDeleted not received")
	}

	// Poll 3: uuid-002 reappears (would be handled by normal poll, not detectMissing)
	// But detectMissing should not emit another IsDeleted
	currentSeen3 := map[string]struct{}{"uuid-001": {}, "uuid-002": {}}
	observer.detectMissing(ctx, currentSeen3, time.Now())

	select {
	case obs := <-obsCh:
		t.Errorf("unexpected observation: %s", obs.UUID)
	case <-time.After(10 * time.Millisecond):
		// expected - no IsDeleted when VM reappears
	}
}

func drainCh(ch chan types.Observation) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
