// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	operatorapi "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/integration/operatorapi"
)

// HarnessServer provides REST API for test orchestration.
type HarnessServer struct {
	libvirtState *fakelibvirt.FakeState
	fakeK8s      *fakek8s.Server
	socketPath   string
	traceStore   *operatorapi.TraceStore
	repoRoot     string

	discovery   *TestDiscovery
	sourceIndex *TestSourceIndex
	currentTest *RunningTest
	results     []TestResult
	events      *EventBroadcaster

	// Test queue
	testQueue   []TestInfo
	queuePaused bool
	queueStop   bool

	mu sync.RWMutex
}

// TestInfo describes an available test.
type TestInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TestFunc    string `json:"test_func"`  // Go test function name
	Group       string `json:"group"`      // integration, unit, benchmark, simulation
	Package     string `json:"package"`    // Go package path (for unit tests)
	ShortPkg    string `json:"short_pkg"`  // Short package name for display
	FilePath    string `json:"file_path,omitempty"`   // Source file path
	Line        int    `json:"line,omitempty"`        // Line number in source file
}

// RunningTest tracks current test execution.
type RunningTest struct {
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	Progress  int       `json:"progress"`
	Step      string    `json:"step"`
	Cancel    context.CancelFunc
}

// TestResult holds test outcome.
type TestResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // passed, failed, stopped
	Duration  int64  `json:"duration"`
	Error     string `json:"error,omitempty"`
	Output    string `json:"output,omitempty"`
	Timestamp string `json:"timestamp"`
	TraceID   int    `json:"trace_id,omitempty"`
}

// NewHarnessServer creates a new harness server.
func NewHarnessServer(libvirtState *fakelibvirt.FakeState, fakeK8s *fakek8s.Server, socketPath string, traceStore *operatorapi.TraceStore, repoRoot string) *HarnessServer {
	s := &HarnessServer{
		libvirtState: libvirtState,
		fakeK8s:      fakeK8s,
		socketPath:   socketPath,
		traceStore:   traceStore,
		repoRoot:     repoRoot,
		events:       NewEventBroadcaster("harness"),
		discovery:    NewTestDiscovery(repoRoot),
		sourceIndex:  NewTestSourceIndex(repoRoot),
	}

	// Run initial discovery in background
	go func() {
		log.Println("[harness] Discovering tests...")
		if err := s.discovery.Refresh(); err != nil {
			log.Printf("[harness] Test discovery error: %v", err)
		}
		tests := s.discovery.Tests()
		log.Printf("[harness] Discovered %d tests", len(tests))

		// Index source locations
		log.Println("[harness] Indexing test source locations...")
		s.sourceIndex.IndexFromDiscovery(tests)
		log.Printf("[harness] Indexed %d test locations", len(s.sourceIndex.locations))

		// Add live simulation (not discoverable via go test)
		s.addSpecialTests()

		s.events.Broadcast("tests_refreshed", map[string]any{
			"count": len(s.discovery.Tests()),
		})
	}()

	return s
}

// addSpecialTests adds tests that aren't discoverable via go test -list.
func (s *HarnessServer) addSpecialTests() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Live simulation runs in the demo process, not via go test
	liveSimulation := TestInfo{
		Name:        "LiveSimulation",
		Description: "Live simulation using the demo's operator (traces operator activity)",
		TestFunc:    "_live_",
		Group:       "simulation",
		Package:     "",
		ShortPkg:    "demo",
	}

	// Check if already exists
	tests := s.discovery.Tests()
	for _, t := range tests {
		if t.Name == liveSimulation.Name {
			return
		}
	}

	// Add to discovery cache (bit of a hack, but works)
	s.discovery.mu.Lock()
	s.discovery.tests = append(s.discovery.tests, liveSimulation)
	s.discovery.mu.Unlock()
}

// ListenAndServe starts the HTTP server.
func (s *HarnessServer) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return http.ListenAndServe(addr, corsMiddleware(mux))
}

func (s *HarnessServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/components", s.handleComponents)
	mux.HandleFunc("/api/tests", s.handleTests)
	mux.HandleFunc("/api/tests/current", s.handleCurrentTest)
	mux.HandleFunc("/api/tests/results", s.handleTestResults)
	mux.HandleFunc("/api/tests/refresh", s.handleTestsRefresh)
	mux.HandleFunc("/api/tests/queue", s.handleTestQueue)
	mux.HandleFunc("/api/tests/source/", s.handleTestSource)
	mux.HandleFunc("/api/events", s.handleEvents)
}

func (s *HarnessServer) handleTestResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check for index parameter to get single result with full output
	indexStr := r.URL.Query().Get("index")
	if indexStr != "" {
		var index int
		if _, err := fmt.Sscanf(indexStr, "%d", &index); err == nil {
			s.mu.RLock()
			if index >= 0 && index < len(s.results) {
				result := s.results[index]
				s.mu.RUnlock()
				writeJSON(w, result)
				return
			}
			s.mu.RUnlock()
			writeError(w, http.StatusNotFound, "result not found")
			return
		}
	}

	// Return list without output (to keep response small)
	s.mu.RLock()
	results := make([]map[string]any, len(s.results))
	for i, r := range s.results {
		results[i] = map[string]any{
			"name":      r.Name,
			"status":    r.Status,
			"duration":  r.Duration,
			"error":     r.Error,
			"timestamp": r.Timestamp,
			"trace_id":  r.TraceID,
		}
	}
	s.mu.RUnlock()

	writeJSON(w, results)
}

func (s *HarnessServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	running := s.currentTest != nil
	s.mu.RUnlock()

	tests := s.discovery.Tests()
	writeJSON(w, map[string]any{
		"status":        "ok",
		"test_running":  running,
		"tests_count":   len(tests),
		"last_scan":     s.discovery.LastScan(),
		"discovery_err": s.discovery.ScanError(),
	})
}

func (s *HarnessServer) handleComponents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"fake_libvirt": map[string]any{
			"status":  "running",
			"domains": len(s.libvirtState.ListDomains()),
		},
		"fake_k8s": map[string]any{
			"status": "running",
		},
		"operator": map[string]any{
			"status": "running",
		},
	})
}

func (s *HarnessServer) handleTestsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	log.Println("[harness] Refreshing test discovery...")
	if err := s.discovery.Refresh(); err != nil {
		log.Printf("[harness] Test discovery error: %v", err)
	}
	s.addSpecialTests()

	tests := s.discovery.Tests()
	log.Printf("[harness] Discovered %d tests", len(tests))

	s.events.Broadcast("tests_refreshed", map[string]any{
		"count": len(tests),
	})

	writeJSON(w, map[string]any{
		"count":     len(tests),
		"last_scan": s.discovery.LastScan(),
	})
}

func (s *HarnessServer) handleTests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tests := s.discovery.Tests()
		writeJSON(w, tests)

	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Find test
		var test *TestInfo
		tests := s.discovery.Tests()
		for _, t := range tests {
			if t.Name == req.Name {
				test = &t
				break
			}
		}
		if test == nil {
			writeError(w, http.StatusNotFound, "test not found")
			return
		}

		// Check if already running
		s.mu.Lock()
		if s.currentTest != nil {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "a test is already running")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		s.currentTest = &RunningTest{
			Name:      test.Name,
			StartedAt: time.Now(),
			Cancel:    cancel,
		}
		s.mu.Unlock()

		// Run test in background
		go s.runTest(ctx, *test)

		writeJSON(w, map[string]any{
			"status": "started",
			"name":   test.Name,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *HarnessServer) handleCurrentTest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		current := s.currentTest
		s.mu.RUnlock()

		if current == nil {
			writeJSON(w, map[string]any{})
			return
		}

		writeJSON(w, map[string]any{
			"name":       current.Name,
			"started_at": current.StartedAt,
			"progress":   current.Progress,
			"step":       current.Step,
		})

	case http.MethodDelete:
		s.mu.Lock()
		if s.currentTest != nil {
			s.currentTest.Cancel()
			// Result will be recorded by runTest
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *HarnessServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.events.Subscribe()
	defer s.events.Unsubscribe(ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case event := <-ch:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *HarnessServer) runTest(ctx context.Context, test TestInfo) {
	startTime := time.Now()

	// Start a trace for this test run
	traceID := s.traceStore.StartTrace(test.Name)

	// Record test start event
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID:  1,
		TID:  1,
		TS:   0,
		PH:   "B",
		Name: test.Name,
		Cat:  "test",
	})

	s.events.Broadcast("test_started", map[string]any{
		"name":     test.Name,
		"trace_id": traceID,
	})

	s.updateProgress(10, "Preparing test environment")

	// Record preparation event
	prepStart := time.Since(startTime).Microseconds()
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID:  1,
		TID:  2,
		TS:   prepStart,
		PH:   "B",
		Name: "prepare",
		Cat:  "setup",
	})

	// End preparation, start execution
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID:  1,
		TID:  2,
		TS:   time.Since(startTime).Microseconds(),
		PH:   "E",
		Name: "prepare",
		Cat:  "setup",
	})

	var output []byte
	var err error

	if test.TestFunc == "_live_" {
		// Run live simulation using the demo's operator
		s.updateProgress(20, "Running live simulation")
		output, err = s.runLiveSimulation(ctx, startTime, traceID)
	} else {
		// Run the actual Go test via `go test`
		s.updateProgress(20, "Running test: "+test.TestFunc)

		execStart := time.Since(startTime).Microseconds()
		s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
			PID:  1,
			TID:  2,
			TS:   execStart,
			PH:   "B",
			Name: "go test",
			Cat:  "execution",
		})

		// Determine the test directory based on package
		testDir := s.resolveTestDir(test.Package)

		// Create a trace output directory for the test
		traceDir, _ := os.MkdirTemp("", "test-trace-*")
		defer func() {
			// After test, import trace file into our trace store
			s.importTraceFile(traceDir, traceID, startTime)
			os.RemoveAll(traceDir)
		}()

		cmd := exec.CommandContext(ctx, "go", "test", "-v", "-run", "^"+test.TestFunc+"$", "-timeout", "60s")
		cmd.Dir = testDir
		cmd.Env = append(os.Environ(), "TRACE_OUTPUT_DIR="+traceDir)

		output, err = cmd.CombinedOutput()

		// End execution
		s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
			PID:  1,
			TID:  2,
			TS:   time.Since(startTime).Microseconds(),
			PH:   "E",
			Name: "go test",
			Cat:  "execution",
		})
	}

	duration := time.Since(startTime).Milliseconds()

	s.updateProgress(90, "Collecting results")

	result := TestResult{
		Name:      test.Name,
		Duration:  duration,
		Output:    string(output),
		Timestamp: time.Now().Format(time.RFC3339),
		TraceID:   traceID,
	}

	if ctx.Err() != nil {
		result.Status = "stopped"
		result.Error = "test was stopped by user"
	} else if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
	} else {
		result.Status = "passed"
	}

	// End test span
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID:  1,
		TID:  1,
		TS:   time.Since(startTime).Microseconds(),
		PH:   "E",
		Name: test.Name,
		Cat:  "test",
		Args: map[string]any{
			"status": result.Status,
		},
	})
	s.traceStore.EndTrace(traceID)

	s.mu.Lock()
	s.results = append([]TestResult{result}, s.results...)
	if len(s.results) > 50 {
		s.results = s.results[:50]
	}
	s.currentTest = nil
	s.mu.Unlock()

	s.events.Broadcast("test_completed", result)

	log.Printf("Test %s completed: %s (%.1fs), trace_id=%d", test.Name, result.Status, float64(duration)/1000, traceID)
}

func (s *HarnessServer) updateProgress(progress int, step string) {
	s.mu.Lock()
	if s.currentTest != nil {
		s.currentTest.Progress = progress
		s.currentTest.Step = step
	}
	s.mu.Unlock()

	s.events.Broadcast("test_progress", map[string]any{
		"progress": progress,
		"step":     step,
	})
}

// runLiveSimulation runs a live simulation using the demo's operator.
// This creates a domain, changes its state, and deletes it - all traced.
func (s *HarnessServer) runLiveSimulation(ctx context.Context, startTime time.Time, traceID int) ([]byte, error) {
	var output []byte

	// Create a test domain
	s.updateProgress(30, "Creating test domain")
	testUUID := [16]byte{0xaa, 0xbb, 0xcc, 0xdd, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc}
	domain := &fakelibvirt.Domain{
		Name:      "live-test-vm",
		UUID:      testUUID,
		ID:        100,
		State:     fakelibvirt.DomainRunning,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	}

	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID: 1, TID: 3, TS: time.Since(startTime).Microseconds(),
		PH: "i", Name: "create_domain", Cat: "simulation",
		Args: map[string]any{"name": domain.Name},
	})

	s.libvirtState.AddDomain(domain)
	output = append(output, []byte("Created domain live-test-vm\n")...)

	// Wait for operator to discover it
	time.Sleep(6 * time.Second)
	s.updateProgress(50, "Domain discovered")

	// Change state to paused
	s.updateProgress(60, "Pausing domain")
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID: 1, TID: 3, TS: time.Since(startTime).Microseconds(),
		PH: "i", Name: "pause_domain", Cat: "simulation",
		Args: map[string]any{"name": domain.Name},
	})

	s.libvirtState.SetDomainState(testUUID, fakelibvirt.DomainPaused)
	output = append(output, []byte("Paused domain live-test-vm\n")...)

	// Wait for state change to propagate
	time.Sleep(6 * time.Second)
	s.updateProgress(70, "State change propagated")

	// Change state to running
	s.updateProgress(75, "Resuming domain")
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID: 1, TID: 3, TS: time.Since(startTime).Microseconds(),
		PH: "i", Name: "resume_domain", Cat: "simulation",
		Args: map[string]any{"name": domain.Name},
	})

	s.libvirtState.SetDomainState(testUUID, fakelibvirt.DomainRunning)
	output = append(output, []byte("Resumed domain live-test-vm\n")...)

	time.Sleep(6 * time.Second)
	s.updateProgress(80, "Deleting domain")

	// Delete the domain
	s.traceStore.AddEvent(traceID, operatorapi.TraceEvent{
		PID: 1, TID: 3, TS: time.Since(startTime).Microseconds(),
		PH: "i", Name: "delete_domain", Cat: "simulation",
		Args: map[string]any{"name": domain.Name},
	})

	s.libvirtState.RemoveDomain(testUUID)
	output = append(output, []byte("Deleted domain live-test-vm\n")...)

	// Wait for deletion to propagate
	time.Sleep(6 * time.Second)

	output = append(output, []byte("Live simulation completed successfully\n")...)
	return output, nil
}

// importTraceFile imports trace events from a test's trace file into the trace store.
func (s *HarnessServer) importTraceFile(traceDir string, traceID int, baseTime time.Time) {
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		log.Printf("Failed to read trace dir: %v", err)
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		tracePath := filepath.Join(traceDir, entry.Name())
		data, err := os.ReadFile(tracePath)
		if err != nil {
			log.Printf("Failed to read trace file %s: %v", tracePath, err)
			continue
		}

		// Parse Chrome Trace Format
		var traceData struct {
			TraceEvents []operatorapi.TraceEvent `json:"traceEvents"`
		}
		if err := json.Unmarshal(data, &traceData); err != nil {
			log.Printf("Failed to parse trace file %s: %v", tracePath, err)
			continue
		}

		// Import events into our trace store
		for _, event := range traceData.TraceEvents {
			s.traceStore.AddEvent(traceID, event)
		}

		log.Printf("Imported %d trace events from %s", len(traceData.TraceEvents), entry.Name())
	}
}

// resolveTestDir converts a Go import path to a directory path relative to repoRoot.
func (s *HarnessServer) resolveTestDir(pkg string) string {
	if pkg == "" {
		return s.repoRoot
	}

	// Extract relative path from import path
	// e.g., "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/api" -> "operator/api"
	relPath := pkg

	// Common module prefixes to strip
	prefixes := []string{
		"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/",
		"github.com/cobaltcore-dev/lv2k8s/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(pkg, prefix) {
			relPath = strings.TrimPrefix(pkg, prefix)
			break
		}
	}

	// Also try stripping anything before the first known directory
	knownRoots := []string{"operator", "fake-libvirt", "harness", "integration", "pkg"}
	for _, root := range knownRoots {
		if idx := strings.Index(relPath, root); idx >= 0 {
			relPath = relPath[idx:]
			break
		}
	}

	// Construct full path
	fullPath := filepath.Join(s.repoRoot, relPath)
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}

	// Fallback to repoRoot
	log.Printf("[harness] Could not resolve package %s, using repoRoot", pkg)
	return s.repoRoot
}

// handleTestQueue handles test queue operations.
func (s *HarnessServer) handleTestQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Get queue status
		s.mu.RLock()
		queue := make([]map[string]any, len(s.testQueue))
		for i, t := range s.testQueue {
			queue[i] = map[string]any{
				"name":  t.Name,
				"group": t.Group,
			}
		}
		paused := s.queuePaused
		s.mu.RUnlock()

		writeJSON(w, map[string]any{
			"queue":  queue,
			"paused": paused,
			"count":  len(queue),
		})

	case http.MethodPost:
		// Add tests to queue
		var req struct {
			Tests []string `json:"tests"` // test names
			Group string   `json:"group"` // or add all from group
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}

		allTests := s.discovery.Tests()
		var toAdd []TestInfo

		if req.Group != "" {
			// Add all tests from group
			for _, t := range allTests {
				if t.Group == req.Group {
					toAdd = append(toAdd, t)
				}
			}
		} else {
			// Add specific tests
			testMap := make(map[string]TestInfo)
			for _, t := range allTests {
				testMap[t.Name] = t
			}
			for _, name := range req.Tests {
				if t, ok := testMap[name]; ok {
					toAdd = append(toAdd, t)
				}
			}
		}

		s.mu.Lock()
		s.testQueue = append(s.testQueue, toAdd...)
		s.queueStop = false
		queueLen := len(s.testQueue)
		s.mu.Unlock()

		// Start processing queue if not already running
		go s.processQueue()

		s.events.Broadcast("queue_updated", map[string]any{
			"count": queueLen,
			"added": len(toAdd),
		})

		writeJSON(w, map[string]any{
			"added": len(toAdd),
			"count": queueLen,
		})

	case http.MethodPut:
		// Pause/resume queue
		var req struct {
			Action string `json:"action"` // "pause", "resume"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}

		s.mu.Lock()
		switch req.Action {
		case "pause":
			s.queuePaused = true
		case "resume":
			s.queuePaused = false
			go s.processQueue() // Resume processing
		}
		paused := s.queuePaused
		s.mu.Unlock()

		s.events.Broadcast("queue_updated", map[string]any{
			"paused": paused,
		})

		writeJSON(w, map[string]any{"paused": paused})

	case http.MethodDelete:
		// Clear queue or stop processing
		s.mu.Lock()
		s.testQueue = nil
		s.queueStop = true
		s.mu.Unlock()

		s.events.Broadcast("queue_updated", map[string]any{
			"count":   0,
			"cleared": true,
		})

		writeJSON(w, map[string]any{"cleared": true})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// processQueue runs tests from the queue sequentially.
func (s *HarnessServer) processQueue() {
	for {
		s.mu.Lock()
		if s.queueStop || len(s.testQueue) == 0 {
			s.mu.Unlock()
			return
		}
		if s.queuePaused {
			s.mu.Unlock()
			return
		}
		if s.currentTest != nil {
			// Already running a test, wait
			s.mu.Unlock()
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Pop next test from queue
		test := s.testQueue[0]
		s.testQueue = s.testQueue[1:]
		queueLen := len(s.testQueue)
		s.mu.Unlock()

		s.events.Broadcast("queue_updated", map[string]any{
			"count": queueLen,
			"next":  test.Name,
		})

		// Run the test
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.currentTest = &RunningTest{
			Name:      test.Name,
			StartedAt: time.Now(),
			Cancel:    cancel,
		}
		s.mu.Unlock()

		s.runTest(ctx, test)

		// Small delay between tests
		time.Sleep(200 * time.Millisecond)
	}
}

// EventBroadcaster manages SSE subscribers.
type EventBroadcaster struct {
	source      string
	subscribers map[chan map[string]any]struct{}
	mu          sync.RWMutex
}

// NewEventBroadcaster creates a new broadcaster.
func NewEventBroadcaster(source string) *EventBroadcaster {
	return &EventBroadcaster{
		source:      source,
		subscribers: make(map[chan map[string]any]struct{}),
	}
}

// Subscribe returns a channel for events.
func (b *EventBroadcaster) Subscribe() chan map[string]any {
	ch := make(chan map[string]any, 100)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel.
func (b *EventBroadcaster) Unsubscribe(ch chan map[string]any) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends an event to all subscribers.
func (b *EventBroadcaster) Broadcast(eventType string, data any) {
	event := map[string]any{
		"type":      eventType,
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"source":    b.source,
		"data":      data,
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// handleTestSource returns source code for a test.
func (s *HarnessServer) handleTestSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Extract test name from URL: /api/tests/source/{name}
	testName := strings.TrimPrefix(r.URL.Path, "/api/tests/source/")
	if testName == "" {
		writeError(w, http.StatusBadRequest, "test name required")
		return
	}

	// Get source from index
	source, err := s.sourceIndex.GetSource(testName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if source == nil {
		// Check if test exists but has no source (like LiveSimulation)
		tests := s.discovery.Tests()
		for _, t := range tests {
			if t.Name == testName {
				writeJSON(w, map[string]any{
					"name":        testName,
					"description": t.Description,
					"group":       t.Group,
					"package":     t.Package,
					"has_source":  false,
				})
				return
			}
		}
		writeError(w, http.StatusNotFound, "test not found")
		return
	}

	// Find test info for metadata
	var testInfo *TestInfo
	tests := s.discovery.Tests()
	for _, t := range tests {
		if t.Name == testName {
			testInfo = &t
			break
		}
	}

	response := map[string]any{
		"name":        testName,
		"file_path":   source.FilePath,
		"line":        source.Line,
		"end_line":    source.EndLine,
		"file_uri":    source.FileURI,
		"source":      source.Source,
		"source_html": source.SourceHTML,
		"has_source":  true,
	}

	if testInfo != nil {
		response["description"] = testInfo.Description
		response["group"] = testInfo.Group
		response["package"] = testInfo.Package
	}

	writeJSON(w, response)
}
