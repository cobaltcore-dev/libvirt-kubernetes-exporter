// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package fakek8s

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIServer wraps a fakek8s Server with REST API endpoints for the web UI.
type APIServer struct {
	k8s        *Server
	events     *EventBroadcaster
	httpServer *http.Server
	startTime  time.Time
}

// NewAPIServer creates a new API server wrapping the fake k8s server.
func NewAPIServer(k8s *Server) *APIServer {
	api := &APIServer{
		k8s:       k8s,
		events:    NewEventBroadcaster("fake-k8s"),
		startTime: time.Now(),
	}

	// Hook into k8s server to broadcast events
	k8s.SetEventCallback(api.onK8sEvent)

	return api
}

// onK8sEvent is called when k8s server processes a request.
func (a *APIServer) onK8sEvent(eventType string, data any) {
	a.events.Broadcast(eventType, data)
}

// ListenAndServe starts the API server on the given address.
func (a *APIServer) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	a.httpServer = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	return a.httpServer.ListenAndServe()
}

// Close shuts down the API server.
func (a *APIServer) Close() error {
	if a.httpServer != nil {
		return a.httpServer.Close()
	}
	return nil
}

func (a *APIServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/resources", a.handleResources)
	mux.HandleFunc("/api/resources/", a.handleResource)
	mux.HandleFunc("/api/faults", a.handleFaults)
	mux.HandleFunc("/api/faults/", a.handleFault)
	mux.HandleFunc("/api/requests", a.handleRequests)
	mux.HandleFunc("/api/events", a.handleEvents)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/reset", a.handleReset)
}

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

// ResourceJSON is the JSON representation for the API.
type ResourceJSON struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	UID        string            `json:"uid"`
	Created    string            `json:"created"`
	Spec       map[string]any    `json:"spec,omitempty"`
	Status     map[string]any    `json:"status,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

func resourceToJSON(r *Resource) ResourceJSON {
	return ResourceJSON{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Name:       r.Metadata.Name,
		Namespace:  r.Metadata.Namespace,
		UID:        r.Metadata.UID,
		Created:    r.Metadata.CreationTimestamp,
		Spec:       r.Spec,
		Status:     r.Status,
		Labels:     r.Metadata.Labels,
	}
}

// handleResources handles GET /api/resources with optional filters.
func (a *APIServer) handleResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse query filters
	query := r.URL.Query()
	group := query.Get("group")
	version := query.Get("version")
	kind := query.Get("kind")
	namespace := query.Get("namespace")

	// Get all resources and filter
	resources := a.k8s.ListAllResources()
	var result []ResourceJSON

	for _, res := range resources {
		// Apply filters
		if group != "" && !strings.HasPrefix(res.APIVersion, group) {
			continue
		}
		if version != "" && !strings.HasSuffix(res.APIVersion, version) {
			continue
		}
		if kind != "" && res.Kind != kind {
			continue
		}
		if namespace != "" && res.Metadata.Namespace != namespace {
			continue
		}
		result = append(result, resourceToJSON(res))
	}

	writeJSON(w, http.StatusOK, result)
}

// handleResource handles GET/DELETE /api/resources/{group}/{version}/{resource}/{namespace}/{name}
func (a *APIServer) handleResource(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/resources/{group}/{version}/{resource}/{namespace}/{name}
	path := strings.TrimPrefix(r.URL.Path, "/api/resources/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		writeError(w, http.StatusBadRequest, "invalid path, expected /api/resources/{group}/{version}/{resource}/{namespace}/{name}")
		return
	}

	group, version, resource, namespace, name := parts[0], parts[1], parts[2], parts[3], parts[4]

	switch r.Method {
	case "GET":
		res, ok := a.k8s.GetResource(group, version, resource, namespace, name)
		if !ok {
			writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		writeJSON(w, http.StatusOK, resourceToJSON(res))

	case "DELETE":
		a.k8s.DeleteResource(group, version, resource, namespace, name)
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// FaultJSON is the JSON representation of a fault rule.
type FaultJSON struct {
	Pattern      string `json:"pattern"`
	DelayMs      int64  `json:"delay_ms,omitempty"`
	ErrorCode    int    `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// handleFaults handles GET/POST /api/faults.
func (a *APIServer) handleFaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		faults := a.k8s.ListFaults()
		result := make([]FaultJSON, 0, len(faults))
		for pattern, cfg := range faults {
			result = append(result, FaultJSON{
				Pattern:      pattern,
				DelayMs:      cfg.Delay.Milliseconds(),
				ErrorCode:    cfg.Code,
				ErrorMessage: cfg.ErrorMsg,
			})
		}
		writeJSON(w, http.StatusOK, result)

	case "POST":
		var req FaultJSON
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.Pattern == "" {
			writeError(w, http.StatusBadRequest, "pattern is required")
			return
		}

		cfg := &FaultConfig{
			Delay:    time.Duration(req.DelayMs) * time.Millisecond,
			Code:     req.ErrorCode,
			ErrorMsg: req.ErrorMessage,
		}
		a.k8s.SetFault(req.Pattern, cfg)

		a.events.Broadcast("fault_added", req)
		writeJSON(w, http.StatusCreated, req)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFault handles DELETE /api/faults/{pattern}
func (a *APIServer) handleFault(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	pattern := strings.TrimPrefix(r.URL.Path, "/api/faults/")
	pattern, _ = url.PathUnescape(pattern)

	a.k8s.SetFault(pattern, nil)
	a.events.Broadcast("fault_removed", map[string]string{"pattern": pattern})
	w.WriteHeader(http.StatusNoContent)
}

// handleRequests handles GET/DELETE /api/requests.
func (a *APIServer) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		query := r.URL.Query()
		limit := 100
		if l := query.Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		requests := a.k8s.Requests()

		// Apply limit (return most recent)
		if len(requests) > limit {
			requests = requests[len(requests)-limit:]
		}

		// Filter by method/path if specified
		method := query.Get("method")
		pathFilter := query.Get("path")

		var result []RequestLog
		for _, req := range requests {
			if method != "" && req.Method != method {
				continue
			}
			if pathFilter != "" && !strings.Contains(req.Path, pathFilter) {
				continue
			}
			result = append(result, req)
		}

		writeJSON(w, http.StatusOK, result)

	case "DELETE":
		a.k8s.ClearRequests()
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleEvents handles SSE endpoint for resource changes.
func (a *APIServer) handleEvents(w http.ResponseWriter, r *http.Request) {
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

	ch := a.events.Subscribe()
	defer a.events.Unsubscribe(ch)

	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected","source":"fake-k8s"}`)
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
func (a *APIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resources := a.k8s.ListAllResources()
	faults := a.k8s.ListFaults()
	requests := a.k8s.Requests()

	// Count resources by kind
	byKind := make(map[string]int)
	for _, res := range resources {
		byKind[res.Kind]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"component":       "fake-k8s",
		"uptime_seconds":  time.Since(a.startTime).Seconds(),
		"resource_count":  len(resources),
		"resources_by_kind": byKind,
		"fault_count":     len(faults),
		"request_count":   len(requests),
		"sse_subscribers": a.events.SubscriberCount(),
	})
}

// handleReset clears all state.
func (a *APIServer) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	a.k8s.Reset()
	a.events.Broadcast("reset", nil)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// SSE support (same pattern as fake-libvirt)

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
