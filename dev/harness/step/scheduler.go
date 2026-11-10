// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package step provides a stepping framework for deterministic concurrency testing.
//
// The core idea is to inject "step gates" into concurrent code. In production,
// these gates are no-ops. In testing, they block until the test harness explicitly
// releases them, allowing deterministic control over goroutine interleaving.
package step

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Point identifies a step location in the code.
// Format: "component:action" e.g., "observer:poll-start", "exporter:k8s-write"
type Point string

// Arrival represents a goroutine that has arrived at a step point and is waiting.
type Arrival struct {
	Point     Point
	WorkerID  string
	Timestamp time.Time
	permit    chan struct{} // closed to release the goroutine
}

// Scheduler controls goroutine execution at step points.
type Scheduler interface {
	// Step blocks until the scheduler permits continuation.
	// In production (NoOpScheduler), this returns immediately.
	// In testing (SteppingScheduler), this blocks until Release is called.
	// The workerID identifies which goroutine is at this step (for logging/control).
	// Returns error if context is cancelled.
	Step(ctx context.Context, workerID string, point Point) error

	// Enabled returns true if stepping is active (not a no-op).
	Enabled() bool
}

// NoOpScheduler implements Scheduler with zero overhead.
// Use this in production.
type NoOpScheduler struct{}

func (NoOpScheduler) Step(ctx context.Context, workerID string, point Point) error {
	return nil
}

func (NoOpScheduler) Enabled() bool {
	return false
}

// Ensure NoOpScheduler implements Scheduler
var _ Scheduler = NoOpScheduler{}

// SteppingScheduler blocks goroutines at step points until explicitly released.
// Use this in tests for deterministic concurrency control.
type SteppingScheduler struct {
	mu       sync.Mutex
	arrivals map[string]*Arrival // keyed by workerID
	log      []LogEntry

	// Notification channels
	arrivalCh chan *Arrival // sent when a goroutine arrives
}

// LogEntry records a step event for replay/debugging.
type LogEntry struct {
	Timestamp time.Time
	WorkerID  string
	Point     Point
	Action    string // "arrive" or "release"
}

// NewSteppingScheduler creates a new scheduler for testing.
func NewSteppingScheduler() *SteppingScheduler {
	return &SteppingScheduler{
		arrivals:  make(map[string]*Arrival),
		arrivalCh: make(chan *Arrival, 100),
	}
}

func (s *SteppingScheduler) Step(ctx context.Context, workerID string, point Point) error {
	permit := make(chan struct{})

	arrival := &Arrival{
		Point:     point,
		WorkerID:  workerID,
		Timestamp: time.Now(),
		permit:    permit,
	}

	s.mu.Lock()
	s.arrivals[workerID] = arrival
	s.log = append(s.log, LogEntry{
		Timestamp: arrival.Timestamp,
		WorkerID:  workerID,
		Point:     point,
		Action:    "arrive",
	})
	s.mu.Unlock()

	// Notify harness of arrival (non-blocking)
	select {
	case s.arrivalCh <- arrival:
	default:
	}

	// Wait for release or context cancellation
	select {
	case <-permit:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.arrivals, workerID)
		s.mu.Unlock()
		return ctx.Err()
	}
}

func (s *SteppingScheduler) Enabled() bool {
	return true
}

// WaitForArrival blocks until a worker arrives at a step point.
// Returns the arrival or error if timeout/context cancelled.
func (s *SteppingScheduler) WaitForArrival(ctx context.Context, timeout time.Duration) (*Arrival, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case arrival := <-s.arrivalCh:
		return arrival, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout waiting for arrival")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// WaitForWorker blocks until a specific worker arrives at any step point.
func (s *SteppingScheduler) WaitForWorker(ctx context.Context, workerID string, timeout time.Duration) (*Arrival, error) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		arrival, ok := s.arrivals[workerID]
		s.mu.Unlock()
		if ok {
			return arrival, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout waiting for worker %q", workerID)
		}

		// Check for new arrivals
		timer := time.NewTimer(min(remaining, 10*time.Millisecond))
		select {
		case <-s.arrivalCh:
			timer.Stop()
			// New arrival, check again
		case <-timer.C:
			// Periodic check
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

// WaitForPoint blocks until any worker arrives at a specific step point.
func (s *SteppingScheduler) WaitForPoint(ctx context.Context, point Point, timeout time.Duration) (*Arrival, error) {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		for _, arrival := range s.arrivals {
			if arrival.Point == point {
				s.mu.Unlock()
				return arrival, nil
			}
		}
		s.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout waiting for point %q", point)
		}

		timer := time.NewTimer(min(remaining, 10*time.Millisecond))
		select {
		case <-s.arrivalCh:
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

// Release allows a waiting worker to continue.
func (s *SteppingScheduler) Release(workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	arrival, ok := s.arrivals[workerID]
	if !ok {
		return fmt.Errorf("worker %q not waiting at any step point", workerID)
	}

	s.log = append(s.log, LogEntry{
		Timestamp: time.Now(),
		WorkerID:  workerID,
		Point:     arrival.Point,
		Action:    "release",
	})

	close(arrival.permit)
	delete(s.arrivals, workerID)
	return nil
}

// ReleaseAll releases all waiting workers.
func (s *SteppingScheduler) ReleaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for workerID, arrival := range s.arrivals {
		s.log = append(s.log, LogEntry{
			Timestamp: now,
			WorkerID:  workerID,
			Point:     arrival.Point,
			Action:    "release",
		})
		close(arrival.permit)
	}
	s.arrivals = make(map[string]*Arrival)
}

// Waiting returns the list of workers currently blocked at step points.
func (s *SteppingScheduler) Waiting() []Arrival {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Arrival, 0, len(s.arrivals))
	for _, a := range s.arrivals {
		result = append(result, *a)
	}
	return result
}

// Log returns the recorded step events for replay/debugging.
func (s *SteppingScheduler) Log() []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]LogEntry, len(s.log))
	copy(result, s.log)
	return result
}

// Ensure SteppingScheduler implements Scheduler
var _ Scheduler = (*SteppingScheduler)(nil)
