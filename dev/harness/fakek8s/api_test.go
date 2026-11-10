// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package fakek8s

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIListResources(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	// Add some test resources
	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "test-domain-1", Namespace: "test-ns"},
		Status:     map[string]any{"state": "running"},
	})
	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "test-domain-2", Namespace: "test-ns"},
		Status:     map[string]any{"state": "shutoff"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/resources", api.handleResources)

	req := httptest.NewRequest("GET", "/api/resources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resources []ResourceJSON
	json.NewDecoder(w.Body).Decode(&resources)

	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resources))
	}
}

func TestAPIListResourcesWithFilters(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	// Add resources in different namespaces
	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "ns1", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "domain-ns1", Namespace: "ns1"},
	})
	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "ns2", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "domain-ns2", Namespace: "ns2"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/resources", api.handleResources)

	// Filter by namespace
	req := httptest.NewRequest("GET", "/api/resources?namespace=ns1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resources []ResourceJSON
	json.NewDecoder(w.Body).Decode(&resources)

	if len(resources) != 1 {
		t.Errorf("expected 1 resource in ns1, got %d", len(resources))
	}
	if resources[0].Namespace != "ns1" {
		t.Errorf("expected namespace ns1, got %s", resources[0].Namespace)
	}
}

func TestAPIGetResource(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "my-domain", Namespace: "test-ns", UID: "abc123"},
		Status:     map[string]any{"state": "running"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/resources/", api.handleResource)

	req := httptest.NewRequest("GET", "/api/resources/kvm.cloud.sap/v1alpha1/libvirtdomains/test-ns/my-domain", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res ResourceJSON
	json.NewDecoder(w.Body).Decode(&res)

	if res.Name != "my-domain" {
		t.Errorf("expected name 'my-domain', got %s", res.Name)
	}
}

func TestAPIDeleteResource(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "to-delete", Namespace: "test-ns"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/resources/", api.handleResource)

	req := httptest.NewRequest("DELETE", "/api/resources/kvm.cloud.sap/v1alpha1/libvirtdomains/test-ns/to-delete", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify deletion
	if _, ok := k8s.GetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", "to-delete"); ok {
		t.Error("resource should have been deleted")
	}
}

func TestAPIFaults(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/faults", api.handleFaults)
	mux.HandleFunc("/api/faults/", api.handleFault)

	// Add a fault
	body := `{"pattern":"libvirtdomains","delay_ms":100,"error_code":500,"error_message":"test error"}`
	req := httptest.NewRequest("POST", "/api/faults", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List faults
	req = httptest.NewRequest("GET", "/api/faults", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var faults []FaultJSON
	json.NewDecoder(w.Body).Decode(&faults)

	if len(faults) != 1 {
		t.Errorf("expected 1 fault, got %d", len(faults))
	}
	if faults[0].ErrorCode != 500 {
		t.Errorf("expected error code 500, got %d", faults[0].ErrorCode)
	}

	// Delete fault
	req = httptest.NewRequest("DELETE", "/api/faults/libvirtdomains", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify deletion
	faults = nil
	req = httptest.NewRequest("GET", "/api/faults", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&faults)

	if len(faults) != 0 {
		t.Errorf("expected 0 faults after deletion, got %d", len(faults))
	}
}

func TestAPIRequests(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	// Make a request to k8s to generate a log entry
	req := httptest.NewRequest("GET", "/apis/test/v1/resources", nil)
	w := httptest.NewRecorder()
	k8s.ServeHTTP(w, req)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/requests", api.handleRequests)

	// List requests
	req = httptest.NewRequest("GET", "/api/requests", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var requests []RequestLog
	json.NewDecoder(w.Body).Decode(&requests)

	if len(requests) < 1 {
		t.Errorf("expected at least 1 request log, got %d", len(requests))
	}

	// Clear requests
	req = httptest.NewRequest("DELETE", "/api/requests", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify cleared
	req = httptest.NewRequest("GET", "/api/requests", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&requests)

	if len(requests) != 0 {
		t.Errorf("expected 0 requests after clear, got %d", len(requests))
	}
}

func TestAPIStatus(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	k8s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "test-ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "test", Namespace: "test-ns"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", api.handleStatus)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["component"] != "fake-k8s" {
		t.Errorf("expected component 'fake-k8s', got %v", status["component"])
	}
	if status["resource_count"].(float64) != 1 {
		t.Errorf("expected 1 resource, got %v", status["resource_count"])
	}
}

func TestAPIReset(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	// Add resources and faults
	k8s.SetResource("test", "v1", "things", "ns", &Resource{
		Metadata: Metadata{Name: "thing1"},
	})
	k8s.SetFault("things", &FaultConfig{Code: 500})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/reset", api.handleReset)

	req := httptest.NewRequest("POST", "/api/reset", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify everything cleared
	resources := k8s.ListAllResources()
	if len(resources) != 0 {
		t.Errorf("expected 0 resources after reset, got %d", len(resources))
	}

	faults := k8s.ListFaults()
	if len(faults) != 0 {
		t.Errorf("expected 0 faults after reset, got %d", len(faults))
	}
}

func TestAPISSEEvents(t *testing.T) {
	k8s := New()
	defer k8s.Close()
	api := NewAPIServer(k8s)

	// Subscribe to events
	ch := api.events.Subscribe()
	defer api.events.Unsubscribe(ch)

	// Trigger an event by modifying state
	api.events.Broadcast("test_event", map[string]string{"test": "data"})

	select {
	case event := <-ch:
		if event.Type != "test_event" {
			t.Errorf("expected type 'test_event', got %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}
