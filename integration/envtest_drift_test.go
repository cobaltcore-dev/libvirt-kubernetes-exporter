// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// TestEnvtest_DriftCorrected verifies that when a CRD's status is tampered with
// externally the drift-detection controller restores it to the tracker state.
func TestEnvtest_DriftCorrected(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x44, 0x44, 0x44, 0x44, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88, 0x99, 0x99, 0xaa, 0xaa}
	uuid := env.addDomain(t, rawUUID, "vm-drift")

	// Wait for the CRD to be created and reach running state.
	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Tamper the status via a direct JSON merge-patch to the status subresource.
	name := "test-host-" + uuid
	patch := []byte(`{"status":{"state":"tampered","stateCode":99}}`)
	_, err := env.DynClient.Resource(domainGVR2).Patch(
		context.Background(), name,
		k8stypes.MergePatchType, patch,
		metav1.PatchOptions{},
		"status",
	)
	if err != nil {
		t.Fatalf("patch status: %v", err)
	}

	// Drift controller must restore state=running.
	env.waitForDomainStatus(t, uuid, "running", 15*time.Second)
}

// TestEnvtest_DeletedCRDRecreated verifies that when a CRD is deleted externally
// the drift-detection controller recreates it from the tracker state.
func TestEnvtest_DeletedCRDRecreated(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x55, 0x55, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88, 0x99, 0x99, 0xaa, 0xaa, 0xbb, 0xbb}
	uuid := env.addDomain(t, rawUUID, "vm-recreate")

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Delete the CRD externally.
	name := "test-host-" + uuid
	if err := env.DynClient.Resource(domainGVR2).Delete(
		context.Background(), name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete CRD: %v", err)
	}

	// The drift controller detects the missing CRD and recreates it.
	env.waitForDomain(t, uuid, 15*time.Second)
}
