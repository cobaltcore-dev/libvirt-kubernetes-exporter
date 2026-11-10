// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package logcapture provides log interception and broadcasting for SSE streaming.
package logcapture

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// LogEntry represents a single log line.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"`
}

// Subscriber receives log entries.
type Subscriber chan LogEntry

// Capture intercepts writes and broadcasts to subscribers.
type Capture struct {
	mu          sync.RWMutex
	source      string
	buffer      []LogEntry
	bufferSize  int
	subscribers map[Subscriber]struct{}
	original    io.Writer
}

// New creates a new log capture that tees to the original writer.
func New(source string, bufferSize int) *Capture {
	return &Capture{
		source:      source,
		buffer:      make([]LogEntry, 0, bufferSize),
		bufferSize:  bufferSize,
		subscribers: make(map[Subscriber]struct{}),
		original:    os.Stdout,
	}
}

// SetOutput sets the original writer (default: os.Stdout).
func (c *Capture) SetOutput(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.original = w
}

// Write implements io.Writer, capturing log output.
func (c *Capture) Write(p []byte) (n int, err error) {
	// Write to original
	n, err = c.original.Write(p)

	// Create log entry
	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   string(p),
		Source:    c.source,
	}

	c.mu.Lock()
	// Add to ring buffer
	if len(c.buffer) >= c.bufferSize {
		c.buffer = c.buffer[1:]
	}
	c.buffer = append(c.buffer, entry)

	// Broadcast to subscribers (non-blocking)
	for sub := range c.subscribers {
		select {
		case sub <- entry:
		default:
			// Subscriber buffer full, skip
		}
	}
	c.mu.Unlock()

	return n, err
}

// Subscribe returns a channel that receives log entries.
// The returned channel has a buffer of 100 entries.
func (c *Capture) Subscribe() Subscriber {
	ch := make(Subscriber, 100)
	c.mu.Lock()
	c.subscribers[ch] = struct{}{}
	c.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (c *Capture) Unsubscribe(ch Subscriber) {
	c.mu.Lock()
	delete(c.subscribers, ch)
	c.mu.Unlock()
	close(ch)
}

// History returns recent log entries from the buffer.
func (c *Capture) History(limit int) []LogEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit <= 0 || limit > len(c.buffer) {
		limit = len(c.buffer)
	}

	start := len(c.buffer) - limit
	result := make([]LogEntry, limit)
	copy(result, c.buffer[start:])
	return result
}

// SubscriberCount returns the number of active subscribers.
func (c *Capture) SubscriberCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.subscribers)
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Data      any       `json:"data"`
}

// NewSSEEvent creates a new SSE event.
func NewSSEEvent(eventType, source string, data any) SSEEvent {
	return SSEEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
		Data:      data,
	}
}

// Format returns the SSE-formatted string for this event.
func (e SSEEvent) Format() string {
	data, err := json.Marshal(e)
	if err != nil {
		return "data: {}\n\n"
	}
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
	event := NewSSEEvent(eventType, b.source, data)
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
