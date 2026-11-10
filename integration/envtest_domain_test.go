// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"testing"
	"time"

	fakelibvirt "github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt"
)

// TestEnvtest_DomainAppears verifies that when a domain is added to libvirt the
// operator creates a LibvirtDomain CRD within the envtest kube-apiserver.
func TestEnvtest_DomainAppears(t *testing.T) {
	env := newEnvtestEnv(t)

	uuid := env.addDomain(t,
		[16]byte{0x11, 0x11, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77},
		"vm-appears",
	)

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)
}

// TestEnvtest_StateChange verifies that a domain state change in libvirt is
// reflected in the CRD status within the envtest kube-apiserver.
func TestEnvtest_StateChange(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x22, 0x22, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88}
	uuid := env.addDomain(t, rawUUID, "vm-state-change")

	// Wait for initial CRD with running state.
	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Change domain to shutoff in fake-libvirt.
	if err := env.FakeLibvirt.State().SetDomainState(rawUUID, fakelibvirt.DomainShutoff); err != nil {
		t.Fatalf("SetDomainState: %v", err)
	}

	// Operator must reflect the change.
	env.waitForDomainStatus(t, uuid, "shutoff", 10*time.Second)
}

// TestEnvtest_DomainDeletion verifies that when a domain is removed from libvirt
// the operator marks the CRD with state=gone and eventually deletes it after the
// short retention period configured in newEnvtestEnv (100 ms).
func TestEnvtest_DomainDeletion(t *testing.T) {
	env := newEnvtestEnv(t)

	rawUUID := [16]byte{0x33, 0x33, 0x33, 0x33, 0x44, 0x44, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88, 0x99, 0x99}
	uuid := env.addDomain(t, rawUUID, "vm-deletion")

	env.waitForDomainStatus(t, uuid, "running", 10*time.Second)

	// Remove the domain from fake-libvirt.
	if err := env.FakeLibvirt.State().RemoveDomain(rawUUID); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}

	// Operator marks it gone first.
	env.waitForDomainStatus(t, uuid, "gone", 10*time.Second)

	// Then the retention controller deletes it (retention=100ms in test env).
	env.waitForDomainGone(t, uuid, 10*time.Second)
}
