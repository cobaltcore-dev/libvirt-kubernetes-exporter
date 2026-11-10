// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestEnvtest_HeartbeatCreated verifies that a heartbeat CRD is created once
// the domain CRD is established and the exporter has its UID.
func TestEnvtest_HeartbeatCreated(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x88, 0x88, 0x88, 0x88, 0x99, 0x99, 0xaa, 0xaa, 0xbb, 0xbb, 0xcc, 0xcc, 0xdd, 0xdd, 0xee, 0xee}
	uuid := env.addDomain(t, rawUUID, "vm-heartbeat")

	// Domain CRD must appear first.
	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Then heartbeat CRD should follow.
	env.waitForHeartbeat(t, uuid, 15*time.Second)
}

// TestEnvtest_HeartbeatOwnerRef verifies that the heartbeat CRD carries an
// owner reference pointing at the corresponding domain CRD.
func TestEnvtest_HeartbeatOwnerRef(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x99, 0x99, 0x99, 0x99, 0xaa, 0xaa, 0xbb, 0xbb, 0xcc, 0xcc, 0xdd, 0xdd, 0xee, 0xee, 0xff, 0xff}
	uuid := env.addDomain(t, rawUUID, "vm-ownerref")

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)
	env.waitForHeartbeat(t, uuid, 15*time.Second)

	name := "test-host-" + uuid

	// Fetch the domain CRD to get its UID.
	domCRD, err := env.DynClient.Resource(domainGVR2).Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get domain CRD: %v", err)
	}
	domUID := string(domCRD.GetUID())
	if domUID == "" {
		t.Fatal("domain CRD has no UID")
	}

	// Fetch the heartbeat CRD.
	hbCRD, err := env.DynClient.Resource(heartbeatGVR2).Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get heartbeat CRD: %v", err)
	}

	// Verify owner references.
	owners, _, _ := unstructured.NestedSlice(hbCRD.Object, "metadata", "ownerReferences")
	if len(owners) == 0 {
		t.Fatal("heartbeat CRD has no owner references")
	}
	ownerUID := ""
	for _, o := range owners {
		ownerMap, ok := o.(map[string]interface{})
		if !ok {
			continue
		}
		if uid, ok := ownerMap["uid"].(string); ok {
			ownerUID = uid
			break
		}
	}
	if ownerUID != domUID {
		t.Errorf("heartbeat owner UID = %q, want domain UID %q", ownerUID, domUID)
	}
}
