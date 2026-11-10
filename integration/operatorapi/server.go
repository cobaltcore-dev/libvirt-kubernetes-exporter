// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package api provides a REST API for monitoring the operator.
package operatorapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/config"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/internal/logging"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/exporter"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/state"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

var logger = logging.For("api")

// Server provides REST API and SSE for operator monitoring.
type Server struct {
	config     *config.Config
	tracker    *state.Tracker
	queue      *exporter.CongestionQueue
	events     *EventBroadcaster
	logs       *LogCapture
	traces     *TraceStore
	httpServer *http.Server
	startTime  time.Time
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, tracker *state.Tracker, queue *exporter.CongestionQueue) *Server {
	s := &Server{
		config:    cfg,
		tracker:   tracker,
		queue:     queue,
		events:    NewEventBroadcaster("operator"),
		logs:      NewLogCapture("operator", 1000),
		traces:    NewTraceStore(50), // keep up to 50 traces
		startTime: time.Now(),
	}
	return s
}

// LogWriter returns an io.Writer that captures logs for SSE streaming.
// Pass this to logging.Setup() to route slog output through the capture buffer.
func (s *Server) LogWriter() io.Writer {
	return s.logs
}

// BroadcastEvent sends an event to all SSE subscribers.
func (s *Server) BroadcastEvent(eventType string, data any) {
	s.events.Broadcast(eventType, data)
}

// TraceStore returns the trace store for recording traces.
func (s *Server) TraceStore() *TraceStore {
	return s.traces
}

// TraceEvent records an event to the active trace (if any).
// This is called by the operator components to record their activity.
func (s *Server) TraceEvent(cat, name, phase string, args map[string]any) {
	s.traces.AddActiveEvent(cat, name, phase, args)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	logger.Info("listening", "addr", addr)
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
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/domains", s.handleDomains)
	mux.HandleFunc("/api/domains/", s.handleDomain)
	mux.HandleFunc("/api/queue", s.handleQueue)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/traces", s.handleTraces)
	mux.HandleFunc("/api/traces/", s.handleTrace)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleStatus returns operator health and component status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domains := s.tracker.ListDomains()

	// Count by state
	byState := make(map[string]int)
	for _, d := range domains {
		stateName := d.Status.Value.String()
		byState[stateName]++
	}

	var queueStats exporter.QueueStats
	if s.queue != nil {
		queueStats = s.queue.Stats()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"component":      "operator",
		"hostname":       s.config.Hostname,
		"uptime_seconds": time.Since(s.startTime).Seconds(),
		"tracker": map[string]any{
			"domain_count":     len(domains),
			"domains_by_state": byState,
		},
		"queue": map[string]any{
			"pending_count":  queueStats.PendingCount,
			"total_enqueued": queueStats.TotalEnqueued,
			"total_dropped":  queueStats.TotalDropped,
		},
		"sse_subscribers": s.events.SubscriberCount() + s.logs.SubscriberCount(),
	})
}

// handleConfig returns current configuration.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"libvirt_socket":   s.config.LibvirtSocket,
		"poll_interval_ms": s.config.PollInterval.Milliseconds(),
		"hostname":         s.config.Hostname,
	})
}

// DomainJSON is the JSON representation of a tracked domain.
type DomainJSON struct {
	UUID      string       `json:"uuid"`
	Name      string       `json:"name"`
	Host      string       `json:"host"`
	State     string       `json:"state"`
	StateCode int          `json:"stateCode"`
	Status    ObservedJSON `json:"status"`
	XMLHash   ObservedJSON `json:"xmlHash"`
	MaxMemKB  uint64       `json:"maxMemKB"`
	MemoryKB  uint64       `json:"memoryKB"`
	VCPUs     uint16       `json:"vcpus"`
	CPUTimeNs uint64       `json:"cpuTimeNs"`
	FirstSeen string       `json:"firstSeen"`
	DeletedAt string       `json:"deletedAt,omitempty"` // set when VM goes missing (retention)
	Version   uint64       `json:"version"`
}

// ObservedJSON represents an observed value with timestamps.
type ObservedJSON struct {
	Value       any    `json:"value"`
	Queried     string `json:"queried"`
	Observed    string `json:"observed"`
	Source      string `json:"source"`
	PrevValue   any    `json:"prevValue,omitempty"`
	PrevQueried string `json:"prevQueried,omitempty"`
}

func domainToJSON(d *types.DomainState) DomainJSON {
	var deletedAt string
	if d.DeletedAt != nil {
		deletedAt = d.DeletedAt.Format(time.RFC3339Nano)
	}

	return DomainJSON{
		UUID:      d.UUID,
		Name:      d.Name,
		Host:      d.Host,
		State:     d.Status.Value.String(),
		StateCode: int(d.Status.Value),
		Status: ObservedJSON{
			Value:       d.Status.Value.String(),
			Queried:     d.Status.Queried.Format(time.RFC3339Nano),
			Observed:    d.Status.Observed.Format(time.RFC3339Nano),
			Source:      string(d.Status.Src),
			PrevValue:   d.Status.PrevValue.String(),
			PrevQueried: formatTimeOrEmpty(d.Status.PrevQueried),
		},
		XMLHash: ObservedJSON{
			Value:    d.XMLHash.Value,
			Queried:  formatTimeOrEmpty(d.XMLHash.Queried),
			Observed: formatTimeOrEmpty(d.XMLHash.Observed),
			Source:   string(d.XMLHash.Src),
		},
		MaxMemKB:  d.MaxMem,
		MemoryKB:  d.Memory,
		VCPUs:     d.NrVirtCPU,
		CPUTimeNs: d.CPUTime,
		FirstSeen: d.FirstSeen.Format(time.RFC3339Nano),
		DeletedAt: deletedAt,
		Version:   d.Version,
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// handleDomains returns all tracked domains.
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domains := s.tracker.ListDomains()
	result := make([]DomainJSON, len(domains))
	for i, d := range domains {
		result[i] = domainToJSON(d)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleDomain returns a single domain by UUID.
func (s *Server) handleDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	uuid := strings.TrimPrefix(r.URL.Path, "/api/domains/")
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "missing UUID")
		return
	}

	domain, ok := s.tracker.GetDomain(uuid)
	if !ok {
		writeError(w, http.StatusNotFound, "domain not found")
		return
	}

	writeJSON(w, http.StatusOK, domainToJSON(domain))
}

// handleQueue returns pending updates in the congestion queue.
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var stats exporter.QueueStats
	var pending []string
	if s.queue != nil {
		stats = s.queue.Stats()
		pending = s.queue.PendingUUIDs()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending_count":  stats.PendingCount,
		"pending_uuids":  pending,
		"total_enqueued": stats.TotalEnqueued,
		"total_dropped":  stats.TotalDropped,
	})
}

// handleLogs handles SSE endpoint for log streaming.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	ch := s.logs.Subscribe()
	defer s.logs.Unsubscribe(ch)

	// Send recent history first
	for _, entry := range s.logs.History(50) {
		event := SSEEvent{
			Type:      "log",
			Timestamp: entry.Timestamp,
			Source:    "operator",
			Data:      entry,
		}
		_, _ = fmt.Fprint(w, event.Format())
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case entry := <-ch:
			event := SSEEvent{
				Type:      "log",
				Timestamp: entry.Timestamp,
				Source:    "operator",
				Data:      entry,
			}
			_, _ = fmt.Fprint(w, event.Format())
			flusher.Flush()
		}
	}
}

// handleEvents handles SSE endpoint for domain events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected","source":"operator"}`)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			_, _ = fmt.Fprint(w, event.Format())
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error("failed to encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// SSE and Log capture types

type SSEEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Data      any       `json:"data"`
}

func (e SSEEvent) Format() string {
	data, err := json.Marshal(e)
	if err != nil {
		return "data: {}\n\n"
	}
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

// LogEntry represents a captured log line.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// LogCapture captures log output and broadcasts to subscribers.
type LogCapture struct {
	mu          sync.RWMutex
	source      string
	buffer      []LogEntry
	bufferSize  int
	subscribers map[chan LogEntry]struct{}
	original    io.Writer
}

func NewLogCapture(source string, bufferSize int) *LogCapture {
	return &LogCapture{
		source:      source,
		buffer:      make([]LogEntry, 0, bufferSize),
		bufferSize:  bufferSize,
		subscribers: make(map[chan LogEntry]struct{}),
		original:    os.Stderr,
	}
}

func (c *LogCapture) Write(p []byte) (n int, err error) {
	// Write to original
	n, err = c.original.Write(p)

	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   strings.TrimSpace(string(p)),
	}

	c.mu.Lock()
	if len(c.buffer) >= c.bufferSize {
		c.buffer = c.buffer[1:]
	}
	c.buffer = append(c.buffer, entry)

	for sub := range c.subscribers {
		select {
		case sub <- entry:
		default:
		}
	}
	c.mu.Unlock()

	return n, err
}

func (c *LogCapture) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 100)
	c.mu.Lock()
	c.subscribers[ch] = struct{}{}
	c.mu.Unlock()
	return ch
}

func (c *LogCapture) Unsubscribe(ch chan LogEntry) {
	c.mu.Lock()
	delete(c.subscribers, ch)
	c.mu.Unlock()
	close(ch)
}

func (c *LogCapture) History(limit int) []LogEntry {
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

func (c *LogCapture) SubscriberCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.subscribers)
}

// TraceStore stores trace sessions in memory for API serving.
type TraceStore struct {
	mu       sync.RWMutex
	traces   []*TraceSession
	maxItems int
	nextID   int
	activeID int       // currently active trace (0 = none)
	baseTime time.Time // start time for timestamp calculation
}

// TraceSession represents a recorded trace.
type TraceSession struct {
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	StartTime time.Time    `json:"start_time"`
	EndTime   *time.Time   `json:"end_time,omitempty"`
	Events    []TraceEvent `json:"traceEvents,omitempty"`
}

// TraceEvent is a Chrome Trace Format event.
type TraceEvent struct {
	PID  int            `json:"pid"`
	TID  int            `json:"tid"`
	TS   int64          `json:"ts"`
	PH   string         `json:"ph"`
	Name string         `json:"name"`
	Cat  string         `json:"cat"`
	Args map[string]any `json:"args,omitempty"`
	Dur  int64          `json:"dur,omitempty"`
}

// NewTraceStore creates a new trace store.
func NewTraceStore(maxItems int) *TraceStore {
	return &TraceStore{
		traces:   make([]*TraceSession, 0),
		maxItems: maxItems,
		nextID:   1,
	}
}

// StartTrace starts a new trace session and returns its ID.
// The new trace becomes the active trace for AddActiveEvent calls.
func (t *TraceStore) StartTrace(name string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.nextID
	t.nextID++

	now := time.Now()
	session := &TraceSession{
		ID:        id,
		Name:      name,
		StartTime: now,
		Events:    make([]TraceEvent, 0),
	}

	t.traces = append(t.traces, session)
	t.activeID = id
	t.baseTime = now

	// Trim old traces
	if len(t.traces) > t.maxItems {
		t.traces = t.traces[len(t.traces)-t.maxItems:]
	}

	return id
}

// EndTrace marks a trace session as complete.
func (t *TraceStore) EndTrace(id int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, tr := range t.traces {
		if tr.ID == id {
			now := time.Now()
			tr.EndTime = &now
			break
		}
	}

	// Clear active trace if this was it
	if t.activeID == id {
		t.activeID = 0
	}
}

// AddActiveEvent adds an event to the currently active trace (if any).
// This is used by the operator to record events during test runs.
// Returns false if no trace is active.
func (t *TraceStore) AddActiveEvent(cat, name, phase string, args map[string]any) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeID == 0 {
		return false
	}

	// Find active trace
	for _, tr := range t.traces {
		if tr.ID == t.activeID {
			// Calculate timestamp in microseconds from trace start
			ts := time.Since(t.baseTime).Microseconds()

			// Assign TID based on category
			tid := 10 // default
			switch cat {
			case "observer":
				tid = 11
			case "tracker":
				tid = 12
			case "exporter":
				tid = 13
			case "heartbeat":
				tid = 14
			}

			tr.Events = append(tr.Events, TraceEvent{
				PID:  1,
				TID:  tid,
				TS:   ts,
				PH:   phase,
				Name: name,
				Cat:  cat,
				Args: args,
			})
			return true
		}
	}
	return false
}

// IsActive returns true if there's an active trace.
func (t *TraceStore) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activeID != 0
}

// AddEvent adds an event to a trace session.
func (t *TraceStore) AddEvent(id int, event TraceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, tr := range t.traces {
		if tr.ID == id {
			tr.Events = append(tr.Events, event)
			break
		}
	}
}

// List returns all trace sessions (without events for listing).
func (t *TraceStore) List() []TraceSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]TraceSession, len(t.traces))
	for i, tr := range t.traces {
		result[i] = TraceSession{
			ID:        tr.ID,
			Name:      tr.Name,
			StartTime: tr.StartTime,
			EndTime:   tr.EndTime,
		}
	}
	return result
}

// Get returns a single trace session with all events.
func (t *TraceStore) Get(id int) *TraceSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, tr := range t.traces {
		if tr.ID == id {
			// Return a copy
			events := make([]TraceEvent, len(tr.Events))
			copy(events, tr.Events)
			return &TraceSession{
				ID:        tr.ID,
				Name:      tr.Name,
				StartTime: tr.StartTime,
				EndTime:   tr.EndTime,
				Events:    events,
			}
		}
	}
	return nil
}

// GetLatest returns the most recent trace session.
func (t *TraceStore) GetLatest() *TraceSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.traces) == 0 {
		return nil
	}
	return t.Get(t.traces[len(t.traces)-1].ID)
}

// handleTraces lists all trace sessions.
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	traces := s.traces.List()
	writeJSON(w, http.StatusOK, traces)
}

// handleTrace returns a single trace by ID.
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	if idStr == "" || idStr == "latest" {
		trace := s.traces.GetLatest()
		if trace == nil {
			writeError(w, http.StatusNotFound, "no traces available")
			return
		}
		writeJSON(w, http.StatusOK, trace)
		return
	}

	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid trace ID")
		return
	}

	trace := s.traces.Get(id)
	if trace == nil {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	}

	writeJSON(w, http.StatusOK, trace)
}
