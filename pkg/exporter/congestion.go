// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"context"
	"sync"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

// CongestionQueue manages updates with stale-dropping.
// If a newer update for the same domain UUID arrives while an older one
// is still pending, the older one is dropped.
type CongestionQueue struct {
	mu            sync.Mutex
	pending       map[string]*types.Update // keyed by UUID, only latest kept
	order         []string                 // FIFO order of UUIDs
	notify        chan struct{}
	totalEnqueued uint64
	totalDropped  uint64
}

// QueueStats holds queue statistics.
type QueueStats struct {
	PendingCount  int
	TotalEnqueued uint64
	TotalDropped  uint64
}

// NewCongestionQueue creates a new congestion-aware queue.
func NewCongestionQueue() *CongestionQueue {
	return &CongestionQueue{
		pending: make(map[string]*types.Update),
		order:   make([]string, 0),
		notify:  make(chan struct{}, 1),
	}
}

// Enqueue adds an update, replacing any older pending update for same UUID.
func (q *CongestionQueue) Enqueue(update types.Update) {
	q.mu.Lock()
	defer q.mu.Unlock()

	uuid := update.Domain.UUID
	q.totalEnqueued++

	if _, exists := q.pending[uuid]; !exists {
		// New UUID - add to order
		q.order = append(q.order, uuid)
	} else {
		// Replacing existing - count as dropped
		q.totalDropped++
	}
	// Always store (or replace) the update
	q.pending[uuid] = &update

	// Signal that there's work
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Dequeue returns the next update to process, blocking if empty.
// Returns nil if context is cancelled.
func (q *CongestionQueue) Dequeue(ctx context.Context) *types.Update {
	for {
		q.mu.Lock()
		if len(q.order) > 0 {
			uuid := q.order[0]
			q.order = q.order[1:]
			update := q.pending[uuid]
			delete(q.pending, uuid)
			q.mu.Unlock()
			return update
		}
		q.mu.Unlock()

		// Wait for notification or context cancellation
		select {
		case <-ctx.Done():
			return nil
		case <-q.notify:
			// Loop back to check again
		}
	}
}

// Len returns the number of pending updates.
func (q *CongestionQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Stats returns queue statistics.
func (q *CongestionQueue) Stats() QueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return QueueStats{
		PendingCount:  len(q.pending),
		TotalEnqueued: q.totalEnqueued,
		TotalDropped:  q.totalDropped,
	}
}

// PendingUUIDs returns the UUIDs of pending updates.
func (q *CongestionQueue) PendingUUIDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]string, len(q.order))
	copy(result, q.order)
	return result
}

// HasPending returns true if there is a pending update for the given UUID.
func (q *CongestionQueue) HasPending(uuid string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, exists := q.pending[uuid]
	return exists
}
