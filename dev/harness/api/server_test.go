// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIStatus(t *testing.T) {
	srv := NewServer()
	srv.RegisterComponent("fake-libvirt", "http://localhost:8081")
	srv.RegisterComponent("fake-k8s", "http://localhost:8082")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["component"] != "harness" {
		t.Errorf("expected component 'harness', got %v", status["component"])
	}
	if !status["all_connected"].(bool) {
		t.Errorf("expected all_connected true")
	}
}

func TestAPIComponents(t *testing.T) {
	srv := NewServer()
	srv.RegisterComponent("fake-libvirt", "http://localhost:8081")
	srv.RegisterComponent("fake-k8s", "http://localhost:8082")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/components", srv.handleComponents)

	req := httptest.NewRequest("GET", "/api/components", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var components []ComponentStatus
	json.NewDecoder(w.Body).Decode(&components)

	if len(components) != 2 {
		t.Errorf("expected 2 components, got %d", len(components))
	}
}

func TestAPIComponentStatusUpdate(t *testing.T) {
	srv := NewServer()
	srv.RegisterComponent("fake-libvirt", "http://localhost:8081")
	srv.UpdateComponentStatus("fake-libvirt", false, "connection refused")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["all_connected"].(bool) {
		t.Errorf("expected all_connected false when component disconnected")
	}
}

func TestAPIListTests(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests", srv.handleTests)

	req := httptest.NewRequest("GET", "/api/tests", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var tests []*TestInfo
	json.NewDecoder(w.Body).Decode(&tests)

	if len(tests) < 5 {
		t.Errorf("expected at least 5 tests, got %d", len(tests))
	}

	// Check that expected tests exist
	found := make(map[string]bool)
	for _, test := range tests {
		found[test.Name] = true
	}

	expectedTests := []string{"basic-discovery", "state-change", "k8s-fault-injection"}
	for _, name := range expectedTests {
		if !found[name] {
			t.Errorf("expected test %s not found", name)
		}
	}
}

func TestAPIRunTest(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests", srv.handleTests)
	mux.HandleFunc("/api/tests/", srv.handleTest)

	// Start a test
	body := `{"name":"basic-discovery"}`
	req := httptest.NewRequest("POST", "/api/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var result TestResult
	json.NewDecoder(w.Body).Decode(&result)

	if result.Name != "basic-discovery" {
		t.Errorf("expected name 'basic-discovery', got %s", result.Name)
	}
	if result.Status != "running" {
		t.Errorf("expected status 'running', got %s", result.Status)
	}

	// Check current test
	req = httptest.NewRequest("GET", "/api/tests/current", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var current map[string]any
	json.NewDecoder(w.Body).Decode(&current)

	if !current["running"].(bool) {
		t.Errorf("expected running true")
	}
	if current["name"] != "basic-discovery" {
		t.Errorf("expected name 'basic-discovery', got %v", current["name"])
	}

	// Wait for test to complete
	time.Sleep(3 * time.Second)

	// Check result
	req = httptest.NewRequest("GET", "/api/tests/basic-discovery", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&result)

	if result.Status != "passed" {
		t.Errorf("expected status 'passed', got %s", result.Status)
	}
	if result.Duration < 1 {
		t.Errorf("expected duration > 1s, got %f", result.Duration)
	}
}

func TestAPIRunUnknownTest(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests", srv.handleTests)

	body := `{"name":"nonexistent-test"}`
	req := httptest.NewRequest("POST", "/api/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAPIRunTestAlreadyRunning(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests", srv.handleTests)

	// Start first test
	body := `{"name":"basic-discovery"}`
	req := httptest.NewRequest("POST", "/api/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Try to start another
	body = `{"name":"state-change"}`
	req = httptest.NewRequest("POST", "/api/tests", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when test already running, got %d", w.Code)
	}

	// Wait for first test to complete
	time.Sleep(3 * time.Second)
}

func TestAPIStopTest(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests", srv.handleTests)
	mux.HandleFunc("/api/tests/", srv.handleTest)

	// Start a test
	body := `{"name":"high-volume"}`
	req := httptest.NewRequest("POST", "/api/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Stop it
	req = httptest.NewRequest("DELETE", "/api/tests/current", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Wait a bit for goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// Check that test was stopped
	result := srv.tests.GetTestResult("high-volume")
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %s", result.Status)
	}
}

func TestAPIStopNoTest(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tests/", srv.handleTest)

	req := httptest.NewRequest("DELETE", "/api/tests/current", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no test running, got %d", w.Code)
	}
}

func TestAPISSEEvents(t *testing.T) {
	srv := NewServer()

	ch := srv.events.Subscribe()
	defer srv.events.Unsubscribe(ch)

	srv.events.Broadcast("test_event", map[string]string{"key": "value"})

	select {
	case event := <-ch:
		if event.Type != "test_event" {
			t.Errorf("expected type 'test_event', got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestAPITestEvents(t *testing.T) {
	srv := NewServer()

	ch := srv.events.Subscribe()
	defer srv.events.Unsubscribe(ch)

	// Start a test - should emit test_started event
	_, err := srv.tests.RunTest("basic-discovery", srv.events)
	if err != nil {
		t.Fatalf("RunTest failed: %v", err)
	}

	// Wait for test_started event
	select {
	case event := <-ch:
		if event.Type != "test_started" {
			t.Errorf("expected type 'test_started', got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for test_started event")
	}

	// Wait for test_completed event
	select {
	case event := <-ch:
		if event.Type != "test_completed" {
			t.Errorf("expected type 'test_completed', got %s", event.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for test_completed event")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := NewServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.HandleFunc("/api/components", srv.handleComponents)
	mux.HandleFunc("/api/events", srv.handleEvents)

	endpoints := []string{"/api/status", "/api/components", "/api/events"}

	for _, ep := range endpoints {
		req := httptest.NewRequest("POST", ep, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", ep, w.Code)
		}
	}
}
