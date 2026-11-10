// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package fakek8s

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestServer_CRD_CRUD(t *testing.T) {
	s := New()
	defer s.Close()

	group := "kvm.cloud.sap"
	version := "v1alpha1"
	resource := "libvirtdomains"
	namespace := "test-ns"
	name := "test-domain"

	// Create resource
	res := Resource{
		APIVersion: group + "/" + version,
		Kind:       "LibvirtDomain",
		Metadata: Metadata{
			Name:      name,
			Namespace: namespace,
		},
		Spec: map[string]any{
			"uuid": "12345-67890",
		},
	}
	body, _ := json.Marshal(res)

	url := s.URL() + "/apis/" + group + "/" + version + "/namespaces/" + namespace + "/" + resource
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	var created Resource
	json.NewDecoder(resp.Body).Decode(&created)
	if created.Metadata.UID == "" {
		t.Error("expected UID to be set")
	}
	if created.Metadata.ResourceVersion == "" {
		t.Error("expected ResourceVersion to be set")
	}

	// Read resource
	resp, err = http.Get(url + "/" + name)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got Resource
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Spec["uuid"] != "12345-67890" {
		t.Errorf("wrong spec.uuid: %v", got.Spec["uuid"])
	}

	// Update resource (PUT)
	got.Spec["state"] = "running"
	body, _ = json.Marshal(got)
	req, _ := http.NewRequest(http.MethodPut, url+"/"+name, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify update
	resp, _ = http.Get(url + "/" + name)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Spec["state"] != "running" {
		t.Errorf("update not applied: %v", got.Spec)
	}

	// Delete resource
	req, _ = http.NewRequest(http.MethodDelete, url+"/"+name, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp, _ = http.Get(url + "/" + name)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestServer_List(t *testing.T) {
	s := New()
	defer s.Close()

	group := "kvm.cloud.sap"
	version := "v1alpha1"
	resource := "libvirtdomains"
	namespace := "test-ns"

	// Create multiple resources
	for i := 0; i < 3; i++ {
		res := Resource{
			APIVersion: group + "/" + version,
			Kind:       "LibvirtDomain",
			Metadata: Metadata{
				Name:      "domain-" + string(rune('a'+i)),
				Namespace: namespace,
			},
		}
		body, _ := json.Marshal(res)
		url := s.URL() + "/apis/" + group + "/" + version + "/namespaces/" + namespace + "/" + resource
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		resp.Body.Close()
	}

	// List resources
	url := s.URL() + "/apis/" + group + "/" + version + "/namespaces/" + namespace + "/" + resource
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET list failed: %v", err)
	}
	defer resp.Body.Close()

	var list ResourceList
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(list.Items))
	}
}

func TestServer_Patch(t *testing.T) {
	s := New()
	defer s.Close()

	group := "kvm.cloud.sap"
	version := "v1alpha1"
	resource := "libvirtdomains"
	namespace := "test-ns"
	name := "patch-test"

	// Create resource
	res := Resource{
		APIVersion: group + "/" + version,
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: name, Namespace: namespace},
		Spec:       map[string]any{"uuid": "123"},
	}
	body, _ := json.Marshal(res)
	url := s.URL() + "/apis/" + group + "/" + version + "/namespaces/" + namespace + "/" + resource
	resp, _ := http.Post(url, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// Patch status
	patch := map[string]any{
		"status": map[string]any{
			"state":    "running",
			"observed": time.Now().Format(time.RFC3339),
		},
	}
	body, _ = json.Marshal(patch)
	req, _ := http.NewRequest(http.MethodPatch, url+"/"+name, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var patched Resource
	json.NewDecoder(resp.Body).Decode(&patched)
	if patched.Status["state"] != "running" {
		t.Errorf("patch not applied: %v", patched.Status)
	}
	if patched.Spec["uuid"] != "123" {
		t.Error("patch overwrote spec")
	}
}

func TestServer_FaultInjection(t *testing.T) {
	s := New()
	defer s.Close()

	group := "kvm.cloud.sap"
	version := "v1alpha1"
	resource := "libvirtdomains"
	namespace := "test-ns"

	// Inject a 500 error for this resource
	s.SetFault("libvirtdomains", &FaultConfig{
		Code:     http.StatusInternalServerError,
		ErrorMsg: "simulated server error",
	})

	url := s.URL() + "/apis/" + group + "/" + version + "/namespaces/" + namespace + "/" + resource
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	// Clear fault
	s.SetFault("libvirtdomains", nil)

	resp, _ = http.Get(url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after clearing fault, got %d", resp.StatusCode)
	}
}

func TestServer_FaultDelay(t *testing.T) {
	s := New()
	defer s.Close()

	s.SetFault("delay-test", &FaultConfig{
		Delay: 50 * time.Millisecond,
	})

	url := s.URL() + "/apis/test/v1/delay-test"
	start := time.Now()
	resp, _ := http.Get(url)
	elapsed := time.Since(start)
	resp.Body.Close()

	if elapsed < 50*time.Millisecond {
		t.Errorf("delay not applied: elapsed=%v", elapsed)
	}
}

func TestServer_RequestLog(t *testing.T) {
	s := New()
	defer s.Close()

	s.ClearRequests()

	url := s.URL() + "/apis/test/v1/namespaces/ns/resources"
	http.Get(url)
	http.Post(url, "application/json", bytes.NewReader([]byte(`{"test":"data"}`)))

	reqs := s.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}
	if reqs[0].Method != "GET" {
		t.Errorf("first request should be GET, got %s", reqs[0].Method)
	}
	if reqs[1].Method != "POST" {
		t.Errorf("second request should be POST, got %s", reqs[1].Method)
	}
	if reqs[1].Body != `{"test":"data"}` {
		t.Errorf("wrong body: %s", reqs[1].Body)
	}
}

func TestServer_DirectAccess(t *testing.T) {
	s := New()
	defer s.Close()

	// SetResource directly
	s.SetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "ns", &Resource{
		APIVersion: "kvm.cloud.sap/v1alpha1",
		Kind:       "LibvirtDomain",
		Metadata:   Metadata{Name: "direct-set", Namespace: "ns"},
		Spec:       map[string]any{"test": "value"},
	})

	// GetResource directly
	res, ok := s.GetResource("kvm.cloud.sap", "v1alpha1", "libvirtdomains", "ns", "direct-set")
	if !ok {
		t.Fatal("resource not found")
	}
	if res.Spec["test"] != "value" {
		t.Errorf("wrong spec: %v", res.Spec)
	}

	// Also verify via HTTP
	url := s.URL() + "/apis/kvm.cloud.sap/v1alpha1/namespaces/ns/libvirtdomains/direct-set"
	resp, _ := http.Get(url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServer_ConflictOnCreate(t *testing.T) {
	s := New()
	defer s.Close()

	res := Resource{
		APIVersion: "test/v1",
		Kind:       "Test",
		Metadata:   Metadata{Name: "dup", Namespace: "ns"},
	}
	body, _ := json.Marshal(res)
	url := s.URL() + "/apis/test/v1/namespaces/ns/tests"

	// First create
	resp, _ := http.Post(url, "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// Duplicate create
	resp, _ = http.Post(url, "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", resp.StatusCode)
	}
}
