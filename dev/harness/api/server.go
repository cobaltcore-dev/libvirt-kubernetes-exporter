// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package api provides a REST API for the test harness orchestrator.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ComponentStatus represents the status of a component.
type ComponentStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// Server provides REST API for test harness orchestration.
type Server struct {
	mu         sync.RWMutex
	components map[string]*ComponentStatus
	events     *EventBroadcaster
	tests      *TestRunner
	httpServer *http.Server
	startTime  time.Time
}

// NewServer creates a new harness API server.
func NewServer() *Server {
	s := &Server{
		components: make(map[string]*ComponentStatus),
		events:     NewEventBroadcaster("harness"),
		tests:      NewTestRunner(),
		startTime:  time.Now(),
	}
	return s
}

// RegisterComponent registers a component for status tracking.
func (s *Server) RegisterComponent(name, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.components[name] = &ComponentStatus{
		Name:      name,
		URL:       url,
		Connected: true,
	}
}

// UpdateComponentStatus updates a component's connection status.
func (s *Server) UpdateComponentStatus(name string, connected bool, err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.components[name]; ok {
		c.Connected = connected
		c.Error = err
	}
}

// BroadcastEvent sends an event to all SSE subscribers.
func (s *Server) BroadcastEvent(eventType string, data any) {
	s.events.Broadcast(eventType, data)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	return s.httpServer.ListenAndServe()
}

// Close shuts down the server.
func (s *Server) Close() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/components", s.handleComponents)
	mux.HandleFunc("/api/tests", s.handleTests)
	mux.HandleFunc("/api/tests/", s.handleTest)
	mux.HandleFunc("/api/events", s.handleEvents)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleStatus returns overall harness status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.RLock()
	components := make([]ComponentStatus, 0, len(s.components))
	allConnected := true
	for _, c := range s.components {
		components = append(components, *c)
		if !c.Connected {
			allConnected = false
		}
	}
	s.mu.RUnlock()

	currentTest := s.tests.CurrentTest()
	var currentTestName string
	if currentTest != nil {
		currentTestName = currentTest.Name
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"component":       "harness",
		"uptime_seconds":  time.Since(s.startTime).Seconds(),
		"components":      components,
		"all_connected":   allConnected,
		"current_test":    currentTestName,
		"sse_subscribers": s.events.SubscriberCount(),
	})
}

// handleComponents returns component status details.
func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.RLock()
	components := make([]ComponentStatus, 0, len(s.components))
	for _, c := range s.components {
		components = append(components, *c)
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, components)
}

// handleTests handles GET /api/tests (list) and POST /api/tests (run).
func (s *Server) handleTests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		tests := s.tests.ListTests()
		writeJSON(w, http.StatusOK, tests)

	case "POST":
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		result, err := s.tests.RunTest(req.Name, s.events)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusAccepted, result)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleTest handles operations on a specific test.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/tests/")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing test name")
		return
	}

	// Handle /api/tests/current specially
	if name == "current" {
		s.handleCurrentTest(w, r)
		return
	}

	switch r.Method {
	case "GET":
		result := s.tests.GetTestResult(name)
		if result == nil {
			writeError(w, http.StatusNotFound, "test not found or not run yet")
			return
		}
		writeJSON(w, http.StatusOK, result)

	case "POST":
		// Start a specific test
		result, err := s.tests.RunTest(name, s.events)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, result)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleCurrentTest handles operations on the current running test.
func (s *Server) handleCurrentTest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		current := s.tests.CurrentTest()
		if current == nil {
			writeJSON(w, http.StatusOK, map[string]any{"running": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"running":    true,
			"name":       current.Name,
			"started_at": current.StartedAt,
		})

	case "DELETE":
		if err := s.tests.StopCurrentTest(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleEvents handles SSE endpoint for aggregated events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ch := s.events.Subscribe()
	defer s.events.Unsubscribe(ch)

	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected","source":"harness"}`)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			fmt.Fprint(w, event.Format())
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// SSE support

type SSEEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Data      any       `json:"data"`
}

func (e SSEEvent) Format() string {
	data, _ := json.Marshal(e)
	return "data: " + string(data) + "\n\n"
}

type EventBroadcaster struct {
	mu          sync.RWMutex
	source      string
	subscribers map[chan SSEEvent]struct{}
}

func NewEventBroadcaster(source string) *EventBroadcaster {
	return &EventBroadcaster{
		source:      source,
		subscribers: make(map[chan SSEEvent]struct{}),
	}
}

func (b *EventBroadcaster) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 100)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBroadcaster) Unsubscribe(ch chan SSEEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

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
		}
	}
}

func (b *EventBroadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// TestRunner manages test execution.
type TestRunner struct {
	mu          sync.RWMutex
	tests       map[string]*TestInfo
	results     map[string]*TestResult
	current     *RunningTest
	cancelFunc  context.CancelFunc
}

// TestInfo describes an available test.
type TestInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// TestResult holds the result of a test run.
type TestResult struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"` // "running", "passed", "failed", "stopped"
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Duration  float64   `json:"duration_seconds,omitempty"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// RunningTest represents a currently running test.
type RunningTest struct {
	Name      string
	StartedAt time.Time
}

// NewTestRunner creates a new test runner.
func NewTestRunner() *TestRunner {
	tr := &TestRunner{
		tests:   make(map[string]*TestInfo),
		results: make(map[string]*TestResult),
	}

	// Register available tests (these match the integration test scenarios)
	tr.tests["basic-discovery"] = &TestInfo{
		Name:        "basic-discovery",
		Description: "Test basic domain discovery and CRD creation",
	}
	tr.tests["state-change"] = &TestInfo{
		Name:        "state-change",
		Description: "Test domain state changes are reflected in CRDs",
	}
	tr.tests["domain-deletion"] = &TestInfo{
		Name:        "domain-deletion",
		Description: "Test domain deletion removes CRDs",
	}
	tr.tests["k8s-fault-injection"] = &TestInfo{
		Name:        "k8s-fault-injection",
		Description: "Test operator resilience to K8s API failures",
	}
	tr.tests["libvirt-reconnection"] = &TestInfo{
		Name:        "libvirt-reconnection",
		Description: "Test operator reconnects after libvirt socket loss",
	}
	tr.tests["high-volume"] = &TestInfo{
		Name:        "high-volume",
		Description: "Test congestion queue under high load",
	}

	return tr
}

// ListTests returns all available tests.
func (tr *TestRunner) ListTests() []*TestInfo {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	result := make([]*TestInfo, 0, len(tr.tests))
	for _, t := range tr.tests {
		result = append(result, t)
	}
	return result
}

// GetTestResult returns the result of a previous test run.
func (tr *TestRunner) GetTestResult(name string) *TestResult {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.results[name]
}

// CurrentTest returns the currently running test, if any.
func (tr *TestRunner) CurrentTest() *RunningTest {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.current
}

// RunTest starts a test by name.
func (tr *TestRunner) RunTest(name string, events *EventBroadcaster) (*TestResult, error) {
	tr.mu.Lock()

	if _, ok := tr.tests[name]; !ok {
		tr.mu.Unlock()
		return nil, fmt.Errorf("unknown test: %s", name)
	}

	if tr.current != nil {
		tr.mu.Unlock()
		return nil, fmt.Errorf("test already running: %s", tr.current.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr.cancelFunc = cancel

	result := &TestResult{
		Name:      name,
		Status:    "running",
		StartedAt: time.Now(),
	}
	tr.results[name] = result
	tr.current = &RunningTest{
		Name:      name,
		StartedAt: result.StartedAt,
	}
	tr.mu.Unlock()

	// Broadcast test started event
	events.Broadcast("test_started", map[string]string{"name": name})

	// Run test in background
	go func() {
		output, err := tr.executeTest(ctx, name)

		tr.mu.Lock()
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(result.StartedAt).Seconds()
		result.Output = output

		if ctx.Err() == context.Canceled {
			result.Status = "stopped"
		} else if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
		} else {
			result.Status = "passed"
		}

		tr.current = nil
		tr.cancelFunc = nil
		tr.mu.Unlock()

		// Broadcast test completed event
		events.Broadcast("test_completed", map[string]any{
			"name":     name,
			"status":   result.Status,
			"duration": result.Duration,
			"error":    result.Error,
		})
	}()

	return result, nil
}

// StopCurrentTest stops the currently running test.
func (tr *TestRunner) StopCurrentTest() error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if tr.current == nil {
		return fmt.Errorf("no test running")
	}

	if tr.cancelFunc != nil {
		tr.cancelFunc()
	}

	return nil
}

// executeTest runs the actual test logic.
// In a real implementation, this would invoke the actual test functions.
// For now, it simulates test execution.
func (tr *TestRunner) executeTest(ctx context.Context, name string) (string, error) {
	// Simulate test execution time
	select {
	case <-ctx.Done():
		return "Test was cancelled", ctx.Err()
	case <-time.After(2 * time.Second):
	}

	// For demonstration, all tests "pass"
	// In a real implementation, this would call the actual test functions
	return fmt.Sprintf("Test %s completed successfully (simulated)", name), nil
}
