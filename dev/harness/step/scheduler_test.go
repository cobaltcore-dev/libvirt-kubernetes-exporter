// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package step

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNoOpScheduler(t *testing.T) {
	sched := NoOpScheduler{}

	if sched.Enabled() {
		t.Error("NoOpScheduler should not be enabled")
	}

	ctx := context.Background()
	err := sched.Step(ctx, "worker1", "test:point")
	if err != nil {
		t.Errorf("NoOpScheduler.Step returned error: %v", err)
	}
}

func TestSteppingScheduler_BasicFlow(t *testing.T) {
	sched := NewSteppingScheduler()

	if !sched.Enabled() {
		t.Error("SteppingScheduler should be enabled")
	}

	ctx := context.Background()

	// Start a worker in a goroutine
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		sched.Step(ctx, "worker1", "test:step1")
	}()

	// Wait for worker to arrive
	arrival, err := sched.WaitForWorker(ctx, "worker1", time.Second)
	if err != nil {
		t.Fatalf("WaitForWorker failed: %v", err)
	}
	if arrival.Point != "test:step1" {
		t.Errorf("wrong point: got %q, want %q", arrival.Point, "test:step1")
	}

	// Worker should be in waiting list
	waiting := sched.Waiting()
	if len(waiting) != 1 {
		t.Errorf("expected 1 waiting, got %d", len(waiting))
	}

	// Release the worker
	err = sched.Release("worker1")
	if err != nil {
		t.Errorf("Release failed: %v", err)
	}

	// Worker should complete
	select {
	case <-workerDone:
		// OK
	case <-time.After(time.Second):
		t.Error("worker did not complete after release")
	}

	// Check log
	log := sched.Log()
	if len(log) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(log))
	}
}

func TestSteppingScheduler_MultipleWorkers(t *testing.T) {
	sched := NewSteppingScheduler()
	ctx := context.Background()

	var wg sync.WaitGroup
	workersDone := make([]chan struct{}, 3)
	for i := 0; i < 3; i++ {
		workersDone[i] = make(chan struct{})
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer close(workersDone[id])
			sched.Step(ctx, workerID(id), Point("test:step"))
		}(i)
	}

	// Wait for all workers to arrive
	for i := 0; i < 3; i++ {
		_, err := sched.WaitForWorker(ctx, workerID(i), time.Second)
		if err != nil {
			t.Fatalf("WaitForWorker(%d) failed: %v", i, err)
		}
	}

	// All 3 should be waiting
	if len(sched.Waiting()) != 3 {
		t.Errorf("expected 3 waiting, got %d", len(sched.Waiting()))
	}

	// Release in specific order: 1, 0, 2
	releaseOrder := []int{1, 0, 2}
	for _, i := range releaseOrder {
		err := sched.Release(workerID(i))
		if err != nil {
			t.Errorf("Release(%d) failed: %v", i, err)
		}

		select {
		case <-workersDone[i]:
			// OK
		case <-time.After(time.Second):
			t.Errorf("worker %d did not complete after release", i)
		}
	}

	wg.Wait()
}

func TestSteppingScheduler_WaitForPoint(t *testing.T) {
	sched := NewSteppingScheduler()
	ctx := context.Background()

	go func() {
		sched.Step(ctx, "worker-a", "pointA")
	}()
	go func() {
		sched.Step(ctx, "worker-b", "pointB")
	}()

	// Wait specifically for pointB
	arrival, err := sched.WaitForPoint(ctx, "pointB", time.Second)
	if err != nil {
		t.Fatalf("WaitForPoint failed: %v", err)
	}
	if arrival.WorkerID != "worker-b" {
		t.Errorf("wrong worker: got %q, want %q", arrival.WorkerID, "worker-b")
	}

	sched.ReleaseAll()
}

func TestSteppingScheduler_ContextCancellation(t *testing.T) {
	sched := NewSteppingScheduler()

	ctx, cancel := context.WithCancel(context.Background())

	stepDone := make(chan error, 1)
	go func() {
		stepDone <- sched.Step(ctx, "worker1", "test:step")
	}()

	// Wait for arrival
	_, err := sched.WaitForWorker(context.Background(), "worker1", time.Second)
	if err != nil {
		t.Fatalf("WaitForWorker failed: %v", err)
	}

	// Cancel the context
	cancel()

	// Step should return with context error
	select {
	case err := <-stepDone:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("step did not return after context cancellation")
	}
}

func TestSteppingScheduler_ReleaseAll(t *testing.T) {
	sched := NewSteppingScheduler()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sched.Step(ctx, workerID(id), "test:step")
		}(i)
	}

	// Wait for all to arrive
	for i := 0; i < 5; i++ {
		_, err := sched.WaitForWorker(ctx, workerID(i), time.Second)
		if err != nil {
			t.Fatalf("WaitForWorker(%d) failed: %v", i, err)
		}
	}

	// Release all at once
	sched.ReleaseAll()

	// All should complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Error("workers did not complete after ReleaseAll")
	}
}

func TestSteppingScheduler_ReleaseUnknownWorker(t *testing.T) {
	sched := NewSteppingScheduler()

	err := sched.Release("nonexistent")
	if err == nil {
		t.Error("Release of unknown worker should fail")
	}
}

func workerID(i int) string {
	return "worker" + string(rune('0'+i))
}
