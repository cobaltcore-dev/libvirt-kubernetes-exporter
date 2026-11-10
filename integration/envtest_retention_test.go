// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
)

// TestEnvtest_RetentionExpiry verifies the full retention lifecycle:
//  1. Domain is created and appears as a running CRD.
//  2. Domain is removed from libvirt → CRD transitions to state=gone.
//  3. After the short retention period (100 ms in test env), the CRD is deleted.
func TestEnvtest_RetentionExpiry(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x66, 0x66, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88, 0x99, 0x99, 0xaa, 0xaa, 0xbb, 0xbb, 0xcc, 0xcc}
	uuid := env.addDomain(t, rawUUID, "vm-retention")

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Remove the domain from libvirt to trigger the gone→delete path.
	if err := env.FakeLibvirt.State().RemoveDomain(rawUUID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}

	// CRD must first transition to gone.
	env.waitForDomainStatus(t, uuid, "gone", 10*time.Second)

	// After the 100 ms retention window the retention controller deletes it.
	env.waitForDomainGone(t, uuid, 10*time.Second)
}

// TestEnvtest_RetentionNotExpiredWhileRunning verifies that a running domain is
// never deleted by the retention controller.
func TestEnvtest_RetentionNotExpiredWhileRunning(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x77, 0x77, 0x77, 0x77, 0x88, 0x88, 0x99, 0x99, 0xaa, 0xaa, 0xbb, 0xbb, 0xcc, 0xcc, 0xdd, 0xdd}
	uuid := env.addDomain(t, rawUUID, "vm-keep")

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Wait longer than the retention period — the domain is still running so it
	// must not be deleted.
	time.Sleep(300 * time.Millisecond)

	// Change state to shutoff so we can confirm it was polled without deletion.
	if err := env.FakeLibvirt.State().SetDomainState(rawUUID, fakelibvirt.DomainShutoff); err != nil {
		t.Fatalf("SetDomainState: %v", err)
	}
	env.waitForDomainStatus(t, uuid, "shutoff", 10*time.Second)
}
