// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package fakek8s provides a minimal Kubernetes API server mock for testing.
//
// It supports:
// - Custom Resource CRUD operations
// - Configurable delays and error injection
// - State tracking for test verification
package fakek8s

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Resource represents a Kubernetes resource with metadata and spec/status.
type Resource struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   Metadata          `json:"metadata"`
	Spec       map[string]any    `json:"spec,omitempty"`
	Status     map[string]any    `json:"status,omitempty"`
}

// Metadata represents resource metadata.
type Metadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

// ResourceList represents a list of resources.
type ResourceList struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Metadata   ListMeta   `json:"metadata"`
	Items      []Resource `json:"items"`
}

// ListMeta is metadata for a list response.
type ListMeta struct {
	ResourceVersion string `json:"resourceVersion,omitempty"`
	Continue        string `json:"continue,omitempty"`
}

// ErrorResponse represents a Kubernetes API error.
type ErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Reason     string `json:"reason"`
	Code       int    `json:"code"`
}

// FaultConfig configures fault injection for a resource path.
type FaultConfig struct {
	Delay    time.Duration // Add delay before processing
	ErrorMsg string        // Return this error message
	Code     int           // HTTP status code for error (0 = no error)
}

// RequestLog records an API request for test verification.
type RequestLog struct {
	Timestamp time.Time
	Method    string
	Path      string
	Body      string
}

// Server is a fake Kubernetes API server.
type Server struct {
	mu        sync.RWMutex
	resources map[string]map[string]*Resource // [group/version/resource/namespace] -> [name] -> Resource
	rv        int64                           // global resource version counter

	faults     map[string]*FaultConfig // path pattern -> fault config
	faultsMu   sync.RWMutex

	globalDelay   time.Duration // global delay applied to all requests
	globalDelayMu sync.RWMutex

	conflicts   map[string]int // UUID -> remaining conflict count
	conflictsMu sync.RWMutex

	requests []RequestLog
	requestsMu sync.Mutex

	eventCallback   EventCallback
	eventCallbackMu sync.RWMutex

	server *httptest.Server
}

// New creates a new fake Kubernetes API server.
func New() *Server {
	s := &Server{
		resources: make(map[string]map[string]*Resource),
		faults:    make(map[string]*FaultConfig),
		conflicts: make(map[string]int),
	}
	s.server = httptest.NewServer(s)
	return s
}

// SetDelay sets a global delay for all requests.
func (s *Server) SetDelay(d time.Duration) {
	s.globalDelayMu.Lock()
	defer s.globalDelayMu.Unlock()
	s.globalDelay = d
}

// InjectConflict injects conflicts for a specific UUID.
// The next 'count' write operations for this UUID will return a conflict error.
func (s *Server) InjectConflict(uuid string, count int) {
	s.conflictsMu.Lock()
	defer s.conflictsMu.Unlock()
	s.conflicts[uuid] = count
}

// ClearConflicts removes all injected conflicts.
func (s *Server) ClearConflicts() {
	s.conflictsMu.Lock()
	defer s.conflictsMu.Unlock()
	s.conflicts = make(map[string]int)
}

// hasConflict checks if there's a pending conflict for a UUID and decrements the count.
func (s *Server) hasConflict(name string) bool {
	s.conflictsMu.Lock()
	defer s.conflictsMu.Unlock()

	// Extract UUID from name (hostname-uuid format)
	parts := strings.SplitN(name, "-", 2)
	uuid := name
	if len(parts) == 2 {
		uuid = parts[1]
	}

	if count, ok := s.conflicts[uuid]; ok && count > 0 {
		s.conflicts[uuid] = count - 1
		if s.conflicts[uuid] == 0 {
			delete(s.conflicts, uuid)
		}
		return true
	}
	return false
}

// URL returns the server's URL for use as a kubeconfig server.
func (s *Server) URL() string {
	return s.server.URL
}

// Close shuts down the server.
func (s *Server) Close() {
	s.server.Close()
}

// resourceKey builds a key for the resources map.
func resourceKey(group, version, resource, namespace string) string {
	if namespace == "" {
		return fmt.Sprintf("%s/%s/%s", group, version, resource)
	}
	return fmt.Sprintf("%s/%s/%s/%s", group, version, resource, namespace)
}

var (
	// Match: /apis/{group}/{version}/namespaces/{namespace}/{resource}/{name}/status
	namespacedStatusRE = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/namespaces/([^/]+)/([^/]+)/([^/]+)/status$`)
	// Match: /apis/{group}/{version}/namespaces/{namespace}/{resource}/{name}
	namespacedResourceRE = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/namespaces/([^/]+)/([^/]+)/([^/]+)$`)
	// Match: /apis/{group}/{version}/namespaces/{namespace}/{resource}
	namespacedListRE = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/namespaces/([^/]+)/([^/]+)$`)
	// Match: /apis/{group}/{version}/{resource}/{name}
	clusterResourceRE = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/([^/]+)/([^/]+)$`)
	// Match: /apis/{group}/{version}/{resource}
	clusterListRE = regexp.MustCompile(`^/apis/([^/]+)/([^/]+)/([^/]+)$`)
)

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log request
	body, _ := io.ReadAll(r.Body)
	s.requestsMu.Lock()
	s.requests = append(s.requests, RequestLog{
		Timestamp: time.Now(),
		Method:    r.Method,
		Path:      r.URL.Path,
		Body:      string(body),
	})
	s.requestsMu.Unlock()

	// Apply global delay
	s.globalDelayMu.RLock()
	delay := s.globalDelay
	s.globalDelayMu.RUnlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	// Check for fault injection
	if fault := s.getFault(r.URL.Path); fault != nil {
		if fault.Delay > 0 {
			time.Sleep(fault.Delay)
		}
		if fault.Code != 0 {
			s.writeError(w, fault.Code, fault.ErrorMsg)
			return
		}
	}

	// Route request
	path := r.URL.Path

	// Handle namespaced status subresource
	if m := namespacedStatusRE.FindStringSubmatch(path); m != nil {
		group, version, namespace, resource, name := m[1], m[2], m[3], m[4], m[5]
		s.handleStatusSubresource(w, r, group, version, resource, namespace, name, body)
		return
	}

	// Handle namespaced resources
	if m := namespacedResourceRE.FindStringSubmatch(path); m != nil {
		group, version, namespace, resource, name := m[1], m[2], m[3], m[4], m[5]
		s.handleResource(w, r, group, version, resource, namespace, name, body)
		return
	}

	// Handle namespaced list
	if m := namespacedListRE.FindStringSubmatch(path); m != nil {
		group, version, namespace, resource := m[1], m[2], m[3], m[4]
		s.handleList(w, r, group, version, resource, namespace, body)
		return
	}

	// Handle cluster-scoped resources
	if m := clusterResourceRE.FindStringSubmatch(path); m != nil {
		group, version, resource, name := m[1], m[2], m[3], m[4]
		s.handleResource(w, r, group, version, resource, "", name, body)
		return
	}

	// Handle cluster-scoped list
	if m := clusterListRE.FindStringSubmatch(path); m != nil {
		group, version, resource := m[1], m[2], m[3]
		s.handleList(w, r, group, version, resource, "", body)
		return
	}

	// Handle /api/v1 core resources (simplified)
	if strings.HasPrefix(path, "/api/v1/") {
		// Not implemented for now
		s.writeError(w, http.StatusNotFound, "core API not implemented")
		return
	}

	s.writeError(w, http.StatusNotFound, "path not found: "+path)
}

func (s *Server) handleResource(w http.ResponseWriter, r *http.Request, group, version, resource, namespace, name string, body []byte) {
	key := resourceKey(group, version, resource, namespace)

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		defer s.mu.RUnlock()
		if resources, ok := s.resources[key]; ok {
			if res, ok := resources[name]; ok {
				s.writeJSON(w, http.StatusOK, res)
				return
			}
		}
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, name))

	case http.MethodPut:
		var res Resource
		if err := json.Unmarshal(body, &res); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		s.mu.Lock()
		if _, ok := s.resources[key]; !ok {
			s.resources[key] = make(map[string]*Resource)
		}
		s.rv++
		res.Metadata.ResourceVersion = fmt.Sprintf("%d", s.rv)
		s.resources[key][name] = &res
		s.mu.Unlock()
		s.writeJSON(w, http.StatusOK, &res)
		s.emitEvent("resource_updated", map[string]any{"resource": &res, "method": "PUT"})

	case http.MethodPatch:
		s.mu.Lock()
		defer s.mu.Unlock()
		if resources, ok := s.resources[key]; ok {
			if existing, ok := resources[name]; ok {
				// Apply strategic merge patch (simplified: just update status if present)
				var patch map[string]any
				if err := json.Unmarshal(body, &patch); err != nil {
					s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
					return
				}
				if status, ok := patch["status"].(map[string]any); ok {
					existing.Status = status
				}
				if spec, ok := patch["spec"].(map[string]any); ok {
					existing.Spec = spec
				}
				if metadata, ok := patch["metadata"].(map[string]any); ok {
					if labels, ok := metadata["labels"].(map[string]any); ok {
						if existing.Metadata.Labels == nil {
							existing.Metadata.Labels = make(map[string]string)
						}
						for k, v := range labels {
							existing.Metadata.Labels[k] = fmt.Sprintf("%v", v)
						}
					}
				}
				s.rv++
				existing.Metadata.ResourceVersion = fmt.Sprintf("%d", s.rv)
				s.writeJSON(w, http.StatusOK, existing)
				return
			}
		}
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, name))

	case http.MethodDelete:
		s.mu.Lock()
		if resources, ok := s.resources[key]; ok {
			if res, ok := resources[name]; ok {
				delete(resources, name)
				s.mu.Unlock()
				s.writeJSON(w, http.StatusOK, res)
				s.emitEvent("resource_deleted", map[string]any{"resource": res, "method": "DELETE"})
				return
			}
		}
		s.mu.Unlock()
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, name))

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleStatusSubresource(w http.ResponseWriter, r *http.Request, group, version, resource, namespace, name string, body []byte) {
	if r.Method != http.MethodPut {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	key := resourceKey(group, version, resource, namespace)
	var incoming Resource
	if err := json.Unmarshal(body, &incoming); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resources, ok := s.resources[key]
	if !ok {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, name))
		return
	}
	existing, ok := resources[name]
	if !ok {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, name))
		return
	}
	existing.Status = incoming.Status
	s.rv++
	existing.Metadata.ResourceVersion = fmt.Sprintf("%d", s.rv)
	s.writeJSON(w, http.StatusOK, existing)
	s.emitEvent("resource_updated", map[string]any{"resource": existing, "method": "PUT /status"})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, group, version, resource, namespace string, body []byte) {
	key := resourceKey(group, version, resource, namespace)

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		defer s.mu.RUnlock()
		items := []Resource{}
		if resources, ok := s.resources[key]; ok {
			for _, res := range resources {
				items = append(items, *res)
			}
		}
		list := ResourceList{
			APIVersion: fmt.Sprintf("%s/%s", group, version),
			Kind:       resource + "List",
			Metadata: ListMeta{
				ResourceVersion: fmt.Sprintf("%d", s.rv),
			},
			Items: items,
		}
		s.writeJSON(w, http.StatusOK, &list)

	case http.MethodPost:
		var res Resource
		if err := json.Unmarshal(body, &res); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.resources[key]; !ok {
			s.resources[key] = make(map[string]*Resource)
		}
		name := res.Metadata.Name
		if _, exists := s.resources[key][name]; exists {
			s.writeError(w, http.StatusConflict, fmt.Sprintf("%s %q already exists", resource, name))
			return
		}
		s.rv++
		res.Metadata.ResourceVersion = fmt.Sprintf("%d", s.rv)
		res.Metadata.UID = fmt.Sprintf("uid-%d", s.rv)
		res.Metadata.CreationTimestamp = time.Now().Format(time.RFC3339)
		s.resources[key][name] = &res
		s.writeJSON(w, http.StatusCreated, &res)

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{
		APIVersion: "v1",
		Kind:       "Status",
		Status:     "Failure",
		Message:    message,
		Reason:     http.StatusText(code),
		Code:       code,
	})
}

// SetFault configures fault injection for requests matching the path pattern.
func (s *Server) SetFault(pathPattern string, fault *FaultConfig) {
	s.faultsMu.Lock()
	defer s.faultsMu.Unlock()
	if fault == nil {
		delete(s.faults, pathPattern)
	} else {
		s.faults[pathPattern] = fault
	}
}

func (s *Server) getFault(path string) *FaultConfig {
	s.faultsMu.RLock()
	defer s.faultsMu.RUnlock()
	for pattern, fault := range s.faults {
		if strings.Contains(path, pattern) {
			return fault
		}
	}
	return nil
}

// Requests returns all recorded requests.
func (s *Server) Requests() []RequestLog {
	s.requestsMu.Lock()
	defer s.requestsMu.Unlock()
	result := make([]RequestLog, len(s.requests))
	copy(result, s.requests)
	return result
}

// ClearRequests clears the request log.
func (s *Server) ClearRequests() {
	s.requestsMu.Lock()
	defer s.requestsMu.Unlock()
	s.requests = nil
}

// GetResource retrieves a resource directly (for test verification).
func (s *Server) GetResource(group, version, resource, namespace, name string) (*Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := resourceKey(group, version, resource, namespace)
	if resources, ok := s.resources[key]; ok {
		if res, ok := resources[name]; ok {
			return res, true
		}
	}
	return nil, false
}

// ListResources returns all resources of a given type (for test verification).
func (s *Server) ListResources(group, version, resource, namespace string) []*Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := resourceKey(group, version, resource, namespace)
	var result []*Resource
	if resources, ok := s.resources[key]; ok {
		for _, res := range resources {
			result = append(result, res)
		}
	}
	return result
}

// SetResource directly sets a resource (for test setup).
func (s *Server) SetResource(group, version, resource, namespace string, res *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(group, version, resource, namespace)
	if _, ok := s.resources[key]; !ok {
		s.resources[key] = make(map[string]*Resource)
	}
	s.rv++
	res.Metadata.ResourceVersion = fmt.Sprintf("%d", s.rv)
	s.resources[key][res.Metadata.Name] = res
}

// Reset clears all resources and faults.
func (s *Server) Reset() {
	s.mu.Lock()
	s.faultsMu.Lock()
	s.requestsMu.Lock()
	defer s.mu.Unlock()
	defer s.faultsMu.Unlock()
	defer s.requestsMu.Unlock()

	s.resources = make(map[string]map[string]*Resource)
	s.faults = make(map[string]*FaultConfig)
	s.requests = nil
	s.rv = 0
}

// ListAllResources returns all stored resources across all groups/namespaces.
func (s *Server) ListAllResources() []*Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Resource
	for _, byName := range s.resources {
		for _, res := range byName {
			result = append(result, res)
		}
	}
	return result
}

// ListFaults returns all configured fault rules.
func (s *Server) ListFaults() map[string]*FaultConfig {
	s.faultsMu.RLock()
	defer s.faultsMu.RUnlock()

	result := make(map[string]*FaultConfig, len(s.faults))
	for k, v := range s.faults {
		result[k] = v
	}
	return result
}

// DeleteResource removes a resource.
func (s *Server) DeleteResource(group, version, resource, namespace, name string) {
	key := resourceKey(group, version, resource, namespace)
	s.mu.Lock()
	defer s.mu.Unlock()

	if byName, ok := s.resources[key]; ok {
		delete(byName, name)
	}
}

// EventCallback is called when certain events occur (for API broadcasting).
type EventCallback func(eventType string, data any)

// SetEventCallback sets the event callback for API broadcasting.
func (s *Server) SetEventCallback(cb EventCallback) {
	s.eventCallbackMu.Lock()
	defer s.eventCallbackMu.Unlock()
	s.eventCallback = cb
}

func (s *Server) emitEvent(eventType string, data any) {
	s.eventCallbackMu.RLock()
	cb := s.eventCallback
	s.eventCallbackMu.RUnlock()
	if cb != nil {
		cb(eventType, data)
	}
}
