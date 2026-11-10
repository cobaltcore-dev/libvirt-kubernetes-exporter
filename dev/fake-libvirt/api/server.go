// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package api provides a REST API for controlling fake-libvirt.
package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

// Server provides REST API and SSE for fake-libvirt.
type Server struct {
	state       *state.FakeState
	events      *EventBroadcaster
	startTime   time.Time
	httpServer  *http.Server
	connCount   int
	connCountMu sync.RWMutex
}

// NewServer creates a new API server.
func NewServer(st *state.FakeState) *Server {
	s := &Server{
		state:     st,
		events:    NewEventBroadcaster("fake-libvirt"),
		startTime: time.Now(),
	}

	// Subscribe to state events and rebroadcast
	go s.forwardStateEvents()

	return s
}

// forwardStateEvents subscribes to state lifecycle events and broadcasts them via SSE.
func (s *Server) forwardStateEvents() {
	ch := make(chan state.LifecycleEvent, 100)
	callbackID := s.state.RegisterEventCallback(0, ch) // 0 = all events
	defer s.state.DeregisterEventCallback(callbackID)

	for event := range ch {
		s.events.Broadcast("lifecycle_event", map[string]any{
			"domain":     domainToJSON(event.Domain),
			"event":      event.Event,
			"event_name": eventName(event.Event),
			"detail":     event.Detail,
		})
	}
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	log.Printf("[fake-libvirt-api] listening on %s", addr)
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
	mux.HandleFunc("/api/domains", s.handleDomains)
	mux.HandleFunc("/api/domains/", s.handleDomain)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/status", s.handleStatus)
}

// corsMiddleware adds CORS headers for browser access.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// DomainJSON is the JSON representation of a domain.
type DomainJSON struct {
	UUID            string `json:"uuid"`
	Name            string `json:"name"`
	ID              int32  `json:"id"`
	State           string `json:"state"`
	StateCode       uint8  `json:"stateCode"`
	StateReasonCode int32  `json:"stateReasonCode"`
	MaxMemKB        uint64 `json:"maxMemKB"`
	MemoryKB        uint64 `json:"memoryKB"`
	VCPUs           uint16 `json:"vcpus"`
	CPUTimeNs       uint64 `json:"cpuTimeNs"`
}

func domainToJSON(d *state.Domain) DomainJSON {
	return DomainJSON{
		UUID:        uuidToString(d.UUID),
		Name:        d.Name,
		ID:          d.ID,
		State:       stateName(d.State),
		StateCode:   d.State,
		StateReasonCode: d.StateReason,
		MaxMemKB:    d.MaxMem,
		MemoryKB:    d.Memory,
		VCPUs:       d.NrVirtCPU,
		CPUTimeNs:   d.CPUTime,
	}
}

func uuidToString(uuid [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

func parseUUID(s string) ([16]byte, error) {
	var uuid [16]byte
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return uuid, fmt.Errorf("invalid UUID length")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return uuid, err
	}
	copy(uuid[:], b)
	return uuid, nil
}

func stateName(s uint8) string {
	switch s {
	case state.DomainNostate:
		return "nostate"
	case state.DomainRunning:
		return "running"
	case state.DomainBlocked:
		return "blocked"
	case state.DomainPaused:
		return "paused"
	case state.DomainShutdown:
		return "shutdown"
	case state.DomainShutoff:
		return "shutoff"
	case state.DomainCrashed:
		return "crashed"
	case state.DomainPmsuspended:
		return "pmsuspended"
	default:
		return "unknown"
	}
}

func parseState(s string) (uint8, error) {
	switch strings.ToLower(s) {
	case "nostate":
		return state.DomainNostate, nil
	case "running":
		return state.DomainRunning, nil
	case "blocked":
		return state.DomainBlocked, nil
	case "paused":
		return state.DomainPaused, nil
	case "shutdown":
		return state.DomainShutdown, nil
	case "shutoff":
		return state.DomainShutoff, nil
	case "crashed":
		return state.DomainCrashed, nil
	case "pmsuspended":
		return state.DomainPmsuspended, nil
	default:
		return 0, fmt.Errorf("unknown state: %s", s)
	}
}

func eventName(e int32) string {
	switch e {
	case state.DomainEventDefined:
		return "defined"
	case state.DomainEventUndefined:
		return "undefined"
	case state.DomainEventStarted:
		return "started"
	case state.DomainEventSuspended:
		return "suspended"
	case state.DomainEventResumed:
		return "resumed"
	case state.DomainEventStopped:
		return "stopped"
	case state.DomainEventShutdown:
		return "shutdown"
	case state.DomainEventCrashed:
		return "crashed"
	default:
		return "unknown"
	}
}

// handleDomains handles GET /api/domains (list) and POST /api/domains (create).
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		domains := s.state.ListDomains()
		result := make([]DomainJSON, len(domains))
		for i, d := range domains {
			result[i] = domainToJSON(d)
		}
		writeJSON(w, http.StatusOK, result)

	case "POST":
		var req struct {
			Name      string `json:"name"`
			UUID      string `json:"uuid,omitempty"`
			State     string `json:"state"`
			MaxMemKB  uint64 `json:"maxMemKB"`
			MemoryKB  uint64 `json:"memoryKB"`
			VCPUs     uint16 `json:"vcpus"`
			CPUTimeNs uint64 `json:"cpuTimeNs,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		var uuid [16]byte
		var err error
		if req.UUID != "" {
			uuid, err = parseUUID(req.UUID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid UUID: "+err.Error())
				return
			}
		} else {
			// Generate random UUID
			uuid = generateUUID()
		}

		stateCode, err := parseState(req.State)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		id := int32(-1)
		if stateCode == state.DomainRunning || stateCode == state.DomainPaused {
			id = int32(time.Now().UnixNano() % 1000)
		}

		domain := &state.Domain{
			UUID:      uuid,
			Name:      req.Name,
			ID:        id,
			State:     stateCode,
			MaxMem:    req.MaxMemKB,
			Memory:    req.MemoryKB,
			NrVirtCPU: req.VCPUs,
			CPUTime:   req.CPUTimeNs,
		}

		if err := s.state.AddDomain(domain); err != nil {
			writeError(w, http.StatusConflict, "domain already exists")
			return
		}

		writeJSON(w, http.StatusCreated, domainToJSON(domain))

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleDomain handles operations on a specific domain.
func (s *Server) handleDomain(w http.ResponseWriter, r *http.Request) {
	// Parse UUID from path: /api/domains/{uuid}[/state]
	path := strings.TrimPrefix(r.URL.Path, "/api/domains/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing UUID")
		return
	}

	uuid, err := parseUUID(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid UUID: "+err.Error())
		return
	}

	// Check if this is a state or jobstats operation
	if len(parts) > 1 && parts[1] == "state" {
		s.handleDomainState(w, r, uuid)
		return
	}
	if len(parts) > 1 && parts[1] == "jobstats" {
		s.handleDomainJobStats(w, r, uuid)
		return
	}

	switch r.Method {
	case "GET":
		domain, ok := s.state.GetDomain(uuid)
		if !ok {
			writeError(w, http.StatusNotFound, "domain not found")
			return
		}
		writeJSON(w, http.StatusOK, domainToJSON(domain))

	case "DELETE":
		if err := s.state.RemoveDomain(uuid); err != nil {
			writeError(w, http.StatusNotFound, "domain not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// JobStatsJSON is the JSON representation of a domain's job stats.
type JobStatsJSON struct {
	JobType      int32 `json:"jobType"`
	JobOperation int32 `json:"jobOperation"`
}

func jobStatsToJSON(d *state.Domain) JobStatsJSON {
	return JobStatsJSON{
		JobType:      d.JobType,
		JobOperation: d.JobOperation,
	}
}

// handleDomainJobStats handles GET and PUT /api/domains/{uuid}/jobstats.
// Send jobType=0 to clear an active job.
func (s *Server) handleDomainJobStats(w http.ResponseWriter, r *http.Request, uuid [16]byte) {
	switch r.Method {
	case "GET":
		domain, ok := s.state.GetDomain(uuid)
		if !ok {
			writeError(w, http.StatusNotFound, "domain not found")
			return
		}
		writeJSON(w, http.StatusOK, jobStatsToJSON(domain))
		return
	case "PUT":
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		JobType      int32 `json:"jobType"`
		JobOperation int32 `json:"jobOperation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	d := &state.Domain{
		JobType:      req.JobType,
		JobOperation: req.JobOperation,
	}
	if err := s.state.SetDomainJobStats(uuid, d); err != nil {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDomainState handles PUT /api/domains/{uuid}/state.
func (s *Server) handleDomainState(w http.ResponseWriter, r *http.Request, uuid [16]byte) {
	if r.Method != "PUT" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		State       string `json:"state"`
		StateReason int32  `json:"stateReason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	stateCode, err := parseState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.state.SetDomainStateWithReason(uuid, stateCode, req.StateReason); err != nil {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}

	domain, _ := s.state.GetDomain(uuid)
	writeJSON(w, http.StatusOK, domainToJSON(domain))
}

// handleEvents handles SSE endpoint for lifecycle events.
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

	// Send initial connected event
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected","source":"fake-libvirt"}`)
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

// handleStatus returns server status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domains := s.state.ListDomains()
	byState := make(map[string]int)
	for _, d := range domains {
		byState[stateName(d.State)]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"component":         "fake-libvirt",
		"uptime_seconds":    time.Since(s.startTime).Seconds(),
		"domain_count":      len(domains),
		"domains_by_state":  byState,
		"sse_subscribers":   s.events.SubscriberCount(),
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func generateUUID() [16]byte {
	var uuid [16]byte
	// Use timestamp-based pseudo-random
	ts := time.Now().UnixNano()
	for i := 0; i < 16; i++ {
		uuid[i] = byte((ts >> (i * 4)) & 0xFF)
	}
	// Set version 4 bits
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return uuid
}
