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

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

func TestListDomains(t *testing.T) {
	st := state.New() // Creates 3 test domains
	srv := NewServer(st)

	req := httptest.NewRequest("GET", "/api/domains", nil)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains", srv.handleDomains)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var domains []DomainJSON
	if err := json.NewDecoder(w.Body).Decode(&domains); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(domains) != 3 {
		t.Errorf("expected 3 domains, got %d", len(domains))
	}
}

func TestCreateDomain(t *testing.T) {
	st := state.NewEmpty()
	srv := NewServer(st)

	body := `{"name":"test-create","state":"running","maxMemKB":1024,"memoryKB":512,"vcpus":2}`
	req := httptest.NewRequest("POST", "/api/domains", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains", srv.handleDomains)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var domain DomainJSON
	if err := json.NewDecoder(w.Body).Decode(&domain); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if domain.Name != "test-create" {
		t.Errorf("expected name 'test-create', got %s", domain.Name)
	}
	if domain.State != "running" {
		t.Errorf("expected state 'running', got %s", domain.State)
	}
	if domain.MaxMemKB != 1024 {
		t.Errorf("expected maxMemKB 1024, got %d", domain.MaxMemKB)
	}
}

func TestCreateDomainWithUUID(t *testing.T) {
	st := state.NewEmpty()
	srv := NewServer(st)

	body := `{"name":"test-uuid","uuid":"aaaabbbb-cccc-dddd-eeee-ffffffffffff","state":"shutoff","maxMemKB":2048,"memoryKB":0,"vcpus":1}`
	req := httptest.NewRequest("POST", "/api/domains", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains", srv.handleDomains)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var domain DomainJSON
	json.NewDecoder(w.Body).Decode(&domain)

	if domain.UUID != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Errorf("expected specific UUID, got %s", domain.UUID)
	}
}

func TestGetDomain(t *testing.T) {
	st := state.New()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains/", srv.handleDomain)

	req := httptest.NewRequest("GET", "/api/domains/11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var domain DomainJSON
	json.NewDecoder(w.Body).Decode(&domain)

	if domain.Name != "test-vm-running" {
		t.Errorf("expected name 'test-vm-running', got %s", domain.Name)
	}
}

func TestGetDomainNotFound(t *testing.T) {
	st := state.NewEmpty()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains/", srv.handleDomain)

	req := httptest.NewRequest("GET", "/api/domains/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteDomain(t *testing.T) {
	st := state.New()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains/", srv.handleDomain)

	// Delete existing domain
	req := httptest.NewRequest("DELETE", "/api/domains/11111111-1111-1111-1111-111111111111", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify it's gone
	domains := st.ListDomains()
	if len(domains) != 2 {
		t.Errorf("expected 2 domains after deletion, got %d", len(domains))
	}
}

func TestChangeState(t *testing.T) {
	st := state.New()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains/", srv.handleDomain)

	// Change running domain to paused
	body := `{"state":"paused"}`
	req := httptest.NewRequest("PUT", "/api/domains/11111111-1111-1111-1111-111111111111/state", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var domain DomainJSON
	json.NewDecoder(w.Body).Decode(&domain)

	if domain.State != "paused" {
		t.Errorf("expected state 'paused', got %s", domain.State)
	}
}

func TestInvalidState(t *testing.T) {
	st := state.New()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/domains/", srv.handleDomain)

	body := `{"state":"invalid_state"}`
	req := httptest.NewRequest("PUT", "/api/domains/11111111-1111-1111-1111-111111111111/state", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestStatus(t *testing.T) {
	st := state.New()
	srv := NewServer(st)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", srv.handleStatus)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["component"] != "fake-libvirt" {
		t.Errorf("expected component 'fake-libvirt', got %v", status["component"])
	}
	if status["domain_count"].(float64) != 3 {
		t.Errorf("expected 3 domains, got %v", status["domain_count"])
	}
}

func TestSSEEvents(t *testing.T) {
	st := state.NewEmpty()
	srv := NewServer(st)

	// Give the forwardStateEvents goroutine time to start and register
	time.Sleep(50 * time.Millisecond)

	// Subscribe to events
	ch := srv.events.Subscribe()
	defer srv.events.Unsubscribe(ch)

	// Add a domain (should trigger event)
	uuid := [16]byte{0xAA}
	st.AddDomain(&state.Domain{
		UUID:  uuid,
		Name:  "event-test",
		State: state.DomainRunning,
	})

	// Wait for event
	select {
	case event := <-ch:
		if event.Type != "lifecycle_event" {
			t.Errorf("expected type 'lifecycle_event', got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestUUIDParsing(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"11111111-1111-1111-1111-111111111111", true},
		{"11111111111111111111111111111111", true}, // without dashes
		{"invalid", false},
		{"11111111-1111-1111-1111", false}, // too short
	}

	for _, tc := range tests {
		_, err := parseUUID(tc.input)
		if tc.valid && err != nil {
			t.Errorf("expected valid UUID %s, got error: %v", tc.input, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("expected invalid UUID %s to fail", tc.input)
		}
	}
}

func TestStateParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected uint8
		valid    bool
	}{
		{"running", state.DomainRunning, true},
		{"paused", state.DomainPaused, true},
		{"shutoff", state.DomainShutoff, true},
		{"crashed", state.DomainCrashed, true},
		{"RUNNING", state.DomainRunning, true}, // case insensitive
		{"invalid", 0, false},
	}

	for _, tc := range tests {
		result, err := parseState(tc.input)
		if tc.valid {
			if err != nil {
				t.Errorf("expected valid state %s, got error: %v", tc.input, err)
			}
			if result != tc.expected {
				t.Errorf("expected state code %d for %s, got %d", tc.expected, tc.input, result)
			}
		} else if err == nil {
			t.Errorf("expected invalid state %s to fail", tc.input)
		}
	}
}
