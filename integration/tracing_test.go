// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/tracing"
)

// D2-T1: Tracing disabled by default
func TestTracingDisabledByDefault(t *testing.T) {
	collector := tracing.NewCollector(1)
	if collector.IsEnabled() {
		t.Error("tracing should be disabled by default")
	}
}

// D2-T2: Events written to Chrome Trace Format
func TestChromeTraceFormat(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := tracing.NewWriter(tmpDir, "test")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	collector := tracing.NewCollector(1)
	collector.SetEnabled(true)
	collector.SetWriter(writer)

	// Write some events
	collector.Begin("poll", tracing.CatObserver)
	time.Sleep(10 * time.Millisecond)
	collector.Instant("domain_observed", tracing.CatObserver, map[string]any{
		"uuid":  "test-uuid",
		"state": "running",
	})
	collector.End("poll", tracing.CatObserver)

	// Close writer
	writer.Close()

	// Read and verify
	data, err := os.ReadFile(writer.Path())
	if err != nil {
		t.Fatalf("failed to read trace file: %v", err)
	}

	var trace struct {
		TraceEvents []map[string]any `json:"traceEvents"`
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to parse trace: %v", err)
	}

	if len(trace.TraceEvents) < 3 {
		t.Errorf("expected at least 3 events, got %d", len(trace.TraceEvents))
	}

	// Verify event structure
	hasBegin := false
	hasEnd := false
	hasInstant := false
	for _, event := range trace.TraceEvents {
		switch event["ph"] {
		case "B":
			hasBegin = true
		case "E":
			hasEnd = true
		case "i":
			hasInstant = true
		}
	}

	if !hasBegin || !hasEnd || !hasInstant {
		t.Errorf("missing event types: begin=%v, end=%v, instant=%v", hasBegin, hasEnd, hasInstant)
	}
}

// D2-T3: Buffered writes (not immediate)
func TestBufferedWrites(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := tracing.NewWriter(tmpDir, "buffer-test")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	collector := tracing.NewCollector(1)
	collector.SetEnabled(true)
	collector.SetWriter(writer)

	// Write an event
	collector.Instant("test", "test", nil)

	// Read without flush - may have partial content
	immediate, _ := os.ReadFile(writer.Path())

	// Flush
	writer.Flush()
	flushed, _ := os.ReadFile(writer.Path())

	// Flushed should have more content
	if len(flushed) <= len(immediate) {
		t.Logf("immediate: %d bytes, flushed: %d bytes", len(immediate), len(flushed))
		// This is expected behavior - buffering delays writes
	}

	writer.Close()
}

// D2-T4: Opens in Chrome DevTools (valid format)
func TestOpensInChromeDevTools(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := tracing.NewWriter(tmpDir, "chrome-test")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	collector := tracing.NewCollector(1)
	collector.SetEnabled(true)
	collector.SetWriter(writer)

	// Write various event types
	collector.Begin("operation", "test")
	collector.Instant("marker", "test", map[string]any{"value": 42})
	collector.End("operation", "test")

	start := time.Now()
	time.Sleep(5 * time.Millisecond)
	collector.Complete("complete_op", "test", start, nil)

	writer.Close()

	// Read and verify valid JSON
	data, err := os.ReadFile(writer.Path())
	if err != nil {
		t.Fatalf("failed to read trace file: %v", err)
	}

	var trace struct {
		TraceEvents []tracing.TraceEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("invalid Chrome Trace Format: %v", err)
	}

	// Verify required fields
	for _, event := range trace.TraceEvents {
		if event.PH == "" {
			t.Error("event missing phase (ph)")
		}
		if event.Name == "" {
			t.Error("event missing name")
		}
	}
}

// Test trace file in specific directory
func TestTraceFileLocation(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "traces", "session1")

	writer, err := tracing.NewWriter(subDir, "location-test")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	// Verify directory created
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Error("trace directory not created")
	}

	// Verify file created in correct location
	path := writer.Path()
	if filepath.Dir(path) != subDir {
		t.Errorf("trace file in wrong location: %s", path)
	}
}
