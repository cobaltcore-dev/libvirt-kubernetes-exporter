// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package tracing provides trace event collection in Chrome Trace Format.
package tracing

import (
	"sync"
	"time"
)

// TraceEvent represents a Chrome Trace Format event.
type TraceEvent struct {
	PID  int            `json:"pid"`  // process ID (use host ID for multi-operator)
	TID  int            `json:"tid"`  // thread ID (use track ID)
	TS   int64          `json:"ts"`   // timestamp in microseconds
	PH   string         `json:"ph"`   // phase: B=begin, E=end, i=instant, X=complete
	Name string         `json:"name"` // event name
	Cat  string         `json:"cat"`  // category (observer, tracker, exporter, lifecycle)
	Args map[string]any `json:"args,omitempty"`
	Dur  int64          `json:"dur,omitempty"` // duration for "X" phase events
}

// Collector collects trace events and writes them to storage.
type Collector struct {
	mu      sync.RWMutex
	enabled bool
	writer  *Writer
	pid     int // process ID for this collector
	startTS time.Time
}

// NewCollector creates a new trace collector.
func NewCollector(pid int) *Collector {
	return &Collector{
		pid:     pid,
		startTS: time.Now(),
	}
}

// SetEnabled enables or disables trace collection.
func (c *Collector) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

// SetWriter sets the trace writer.
func (c *Collector) SetWriter(w *Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writer = w
}

// IsEnabled returns whether tracing is enabled.
func (c *Collector) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// Begin starts a span event.
func (c *Collector) Begin(name, category string) {
	c.emit(TraceEvent{
		PID:  c.pid,
		TID:  1,
		TS:   c.timestamp(),
		PH:   "B",
		Name: name,
		Cat:  category,
	})
}

// End ends a span event.
func (c *Collector) End(name, category string) {
	c.emit(TraceEvent{
		PID:  c.pid,
		TID:  1,
		TS:   c.timestamp(),
		PH:   "E",
		Name: name,
		Cat:  category,
	})
}

// Instant records a point event.
func (c *Collector) Instant(name, category string, args map[string]any) {
	c.emit(TraceEvent{
		PID:  c.pid,
		TID:  1,
		TS:   c.timestamp(),
		PH:   "i",
		Name: name,
		Cat:  category,
		Args: args,
	})
}

// Complete records a complete event with duration.
func (c *Collector) Complete(name, category string, start time.Time, args map[string]any) {
	now := time.Now()
	c.emit(TraceEvent{
		PID:  c.pid,
		TID:  1,
		TS:   start.Sub(c.startTS).Microseconds(),
		PH:   "X",
		Name: name,
		Cat:  category,
		Args: args,
		Dur:  now.Sub(start).Microseconds(),
	})
}

// timestamp returns the current time in microseconds since start.
func (c *Collector) timestamp() int64 {
	return time.Since(c.startTS).Microseconds()
}

func (c *Collector) emit(event TraceEvent) {
	c.mu.RLock()
	enabled := c.enabled
	writer := c.writer
	c.mu.RUnlock()

	if !enabled || writer == nil {
		return
	}

	writer.WriteEvent(event)
}

// TrackID assigns tracks for different components.
const (
	TrackObserver = 1
	TrackTracker  = 2
	TrackExporter = 3
	TrackScenario = 4
)

// CategoryNames for different event sources.
const (
	CatObserver  = "observer"
	CatTracker   = "tracker"
	CatExporter  = "exporter"
	CatLifecycle = "lifecycle"
	CatScenario  = "scenario"
)
