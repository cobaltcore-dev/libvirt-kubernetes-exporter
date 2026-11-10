// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"sync"
	"time"
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Data      any       `json:"data"`
}

// Format returns the SSE-formatted string for this event.
func (e SSEEvent) Format() string {
	data, _ := json.Marshal(e)
	return "data: " + string(data) + "\n\n"
}

// EventBroadcaster broadcasts SSE events to multiple subscribers.
type EventBroadcaster struct {
	mu          sync.RWMutex
	source      string
	subscribers map[chan SSEEvent]struct{}
}

// NewEventBroadcaster creates a new event broadcaster.
func NewEventBroadcaster(source string) *EventBroadcaster {
	return &EventBroadcaster{
		source:      source,
		subscribers: make(map[chan SSEEvent]struct{}),
	}
}

// Subscribe returns a channel that receives SSE events.
func (b *EventBroadcaster) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 100)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (b *EventBroadcaster) Unsubscribe(ch chan SSEEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends an event to all subscribers.
func (b *EventBroadcaster) Broadcast(eventType string, data any) {
	event := SSEEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    b.source,
		Data:      data,
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer full, skip
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *EventBroadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
