// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"context"
	"testing"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

func TestCongestionQueueBasic(t *testing.T) {
	q := NewCongestionQueue()

	// Enqueue two updates for different UUIDs
	q.Enqueue(types.Update{
		Domain: &types.DomainState{UUID: "uuid-1", Name: "vm1"},
		Type:   types.UpdateCreated,
	})
	q.Enqueue(types.Update{
		Domain: &types.DomainState{UUID: "uuid-2", Name: "vm2"},
		Type:   types.UpdateCreated,
	})

	if q.Len() != 2 {
		t.Errorf("expected 2 pending, got %d", q.Len())
	}

	ctx := context.Background()

	// Dequeue should return in FIFO order
	u1 := q.Dequeue(ctx)
	if u1.Domain.UUID != "uuid-1" {
		t.Errorf("expected uuid-1, got %s", u1.Domain.UUID)
	}

	u2 := q.Dequeue(ctx)
	if u2.Domain.UUID != "uuid-2" {
		t.Errorf("expected uuid-2, got %s", u2.Domain.UUID)
	}

	if q.Len() != 0 {
		t.Errorf("expected 0 pending, got %d", q.Len())
	}
}

func TestCongestionQueueStaleDropping(t *testing.T) {
	q := NewCongestionQueue()

	// Enqueue three updates for the SAME UUID
	q.Enqueue(types.Update{
		Domain:    &types.DomainState{UUID: "uuid-1", Name: "vm1", Version: 1},
		Type:      types.UpdateHeartbeat,
		Timestamp: time.Now(),
	})
	q.Enqueue(types.Update{
		Domain:    &types.DomainState{UUID: "uuid-1", Name: "vm1", Version: 2},
		Type:      types.UpdateHeartbeat,
		Timestamp: time.Now().Add(time.Second),
	})
	q.Enqueue(types.Update{
		Domain:    &types.DomainState{UUID: "uuid-1", Name: "vm1", Version: 3},
		Type:      types.UpdateHeartbeat,
		Timestamp: time.Now().Add(2 * time.Second),
	})

	// Only ONE should be pending (the latest)
	if q.Len() != 1 {
		t.Errorf("expected 1 pending (stale dropped), got %d", q.Len())
	}

	ctx := context.Background()
	u := q.Dequeue(ctx)

	// Should be the latest version
	if u.Domain.Version != 3 {
		t.Errorf("expected version 3 (latest), got %d", u.Domain.Version)
	}
}

func TestCongestionQueueMixedUUIDs(t *testing.T) {
	q := NewCongestionQueue()

	// Interleaved updates for two UUIDs
	q.Enqueue(types.Update{Domain: &types.DomainState{UUID: "uuid-1", Version: 1}})
	q.Enqueue(types.Update{Domain: &types.DomainState{UUID: "uuid-2", Version: 1}})
	q.Enqueue(types.Update{Domain: &types.DomainState{UUID: "uuid-1", Version: 2}}) // replaces first uuid-1

	// Should have 2 pending (one for each UUID)
	if q.Len() != 2 {
		t.Errorf("expected 2 pending, got %d", q.Len())
	}

	ctx := context.Background()

	// First should be uuid-1 (its slot was created first)
	u1 := q.Dequeue(ctx)
	if u1.Domain.UUID != "uuid-1" || u1.Domain.Version != 2 {
		t.Errorf("expected uuid-1 v2, got %s v%d", u1.Domain.UUID, u1.Domain.Version)
	}

	// Second should be uuid-2
	u2 := q.Dequeue(ctx)
	if u2.Domain.UUID != "uuid-2" || u2.Domain.Version != 1 {
		t.Errorf("expected uuid-2 v1, got %s v%d", u2.Domain.UUID, u2.Domain.Version)
	}
}

func TestCongestionQueueBlocking(t *testing.T) {
	q := NewCongestionQueue()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Dequeue on empty queue should block, then return nil on context cancel
	done := make(chan struct{})
	var result *types.Update
	go func() {
		result = q.Dequeue(ctx)
		close(done)
	}()

	select {
	case <-done:
		if result != nil {
			t.Error("expected nil on cancelled context")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Dequeue didn't return after context cancelled")
	}
}

func TestCongestionQueueNotify(t *testing.T) {
	q := NewCongestionQueue()

	ctx := context.Background()

	// Start a goroutine waiting to dequeue
	done := make(chan *types.Update)
	go func() {
		done <- q.Dequeue(ctx)
	}()

	// Give the goroutine time to start waiting
	time.Sleep(10 * time.Millisecond)

	// Enqueue should wake up the waiter
	q.Enqueue(types.Update{
		Domain: &types.DomainState{UUID: "uuid-1"},
	})

	select {
	case u := <-done:
		if u.Domain.UUID != "uuid-1" {
			t.Errorf("expected uuid-1, got %s", u.Domain.UUID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Dequeue didn't wake up after Enqueue")
	}
}
