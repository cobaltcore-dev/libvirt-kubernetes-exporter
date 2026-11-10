// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestEnvtest_MigrationStats verifies that DomainGetJobStats data flows through
// the operator pipeline and appears in the LibvirtDomain CRD status.migration field.
func TestEnvtest_MigrationStats(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC}
	uuid := env.addDomain(t, rawUUID, "vm-migration-test")

	// Wait for initial CRD to appear.
	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// === Phase 1: Inject an active migration job ===
	t.Log("Phase 1: Setting active migration job stats")
	if err := env.FakeLibvirt.State().SetDomainJobStats(rawUUID, &fakelibvirt.Domain{
		JobType:      1, // DomainJobBounded
		JobOperation: 5, // DomainJobOperationStrMigrationOut → role=source
	}); err != nil {
		t.Fatalf("SetDomainJobStats: %v", err)
	}

	// Wait for migration to appear in CRD.
	env.waitForMigration(t, uuid, "DomainJobBounded", 10*time.Second)

	// Verify role field.
	obj := env.getDomain(t, uuid)
	role, _, _ := unstructured.NestedString(obj.Object, "status", "migration", "role")
	if role != "source" {
		t.Errorf("expected role=source, got %q", role)
	}

	// === Phase 2: Clear the job (migration complete) ===
	t.Log("Phase 2: Clearing migration job")
	if err := env.FakeLibvirt.State().SetDomainJobStats(rawUUID, &fakelibvirt.Domain{
		JobType: 0, // DomainJobNone
	}); err != nil {
		t.Fatalf("SetDomainJobStats clear: %v", err)
	}

	// Wait for migration field to disappear.
	env.waitForMigrationGone(t, uuid, 10*time.Second)
	t.Log("Migration field cleared from CRD")
}

// waitForMigration polls until status.migration.jobType equals want.
func (e *envtestEnv) waitForMigration(t *testing.T, uuid, wantJobType string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	var lastJobType string
	for time.Now().Before(deadline) {
		obj, err := e.DynClient.Resource(domainGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err == nil {
			jt, _, _ := unstructured.NestedString(obj.Object, "status", "migration", "jobType")
			lastJobType = jt
			if jt == wantJobType {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("domain CRD %q status.migration.jobType did not reach %q within %v (last value: %q)", name, wantJobType, timeout, lastJobType)
}

// waitForMigrationGone polls until status.migration is absent.
func (e *envtestEnv) waitForMigrationGone(t *testing.T, uuid string, timeout time.Duration) {
	t.Helper()
	name := "test-host-" + uuid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		obj, err := e.DynClient.Resource(domainGVR2).Get(
			context.Background(), name, metav1.GetOptions{})
		if err == nil {
			_, found, _ := unstructured.NestedMap(obj.Object, "status", "migration")
			if !found {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("domain CRD %q status.migration still present after %v", name, timeout)
}

// getDomain fetches the current CRD object for a domain.
func (e *envtestEnv) getDomain(t *testing.T, uuid string) *unstructured.Unstructured {
	t.Helper()
	name := "test-host-" + uuid
	obj, err := e.DynClient.Resource(domainGVR2).Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getDomain %q: %v", name, err)
	}
	return obj
}
