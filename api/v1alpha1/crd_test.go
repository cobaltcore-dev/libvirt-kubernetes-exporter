// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

const (
	testNovaName    = "example-instance"
	testNovaFlavor  = "m1.large"
	testNovaProject = "example-project"
	testNovaUser    = "example-user"
)

func TestDomainToLibvirtDomain(t *testing.T) {
	domain := &types.DomainState{
		UUID: "test-uuid",
		Name: "test-vm",
		Host: "node001",
		NovaMetadata: types.NovaMetadata{
			Name:    testNovaName,
			Flavor:  testNovaFlavor,
			Project: testNovaProject,
			User:    testNovaUser,
		},
		MaxMem:    2048,
		Memory:    1024,
		NrVirtCPU: 2,
	}
	domain.Status.Update(types.StateRunning, time.Now(), time.Now(), types.SourcePoll)
	domain.XMLHash.Update("abc123", time.Now(), time.Now(), types.SourcePoll)

	crd := DomainToLibvirtDomain(domain)

	if crd.Name != "node001-test-uuid" {
		t.Errorf("expected name node001-test-uuid, got %s", crd.Name)
	}
	if crd.Status.XMLHash != "abc123" {
		t.Errorf("expected XMLHash abc123, got %s", crd.Status.XMLHash)
	}
	if crd.Status.State != "running" {
		t.Errorf("expected state running, got %s", crd.Status.State)
	}
	if crd.Status.NovaMetadata.Name != testNovaName {
		t.Errorf("expected novaMetadata.name %s, got %s", testNovaName, crd.Status.NovaMetadata.Name)
	}
	if crd.Status.NovaMetadata.Flavor != testNovaFlavor {
		t.Errorf("expected novaMetadata.flavor %s, got %s", testNovaFlavor, crd.Status.NovaMetadata.Flavor)
	}
	if crd.Status.NovaMetadata.Project != testNovaProject {
		t.Errorf("expected novaMetadata.project %s, got %s", testNovaProject, crd.Status.NovaMetadata.Project)
	}
	if crd.Status.NovaMetadata.User != testNovaUser {
		t.Errorf("expected novaMetadata.user %s, got %s", testNovaUser, crd.Status.NovaMetadata.User)
	}
	if crd.Labels["libvirt.kvm.cloud.sap/uuid"] != "test-uuid" {
		t.Error("expected uuid label")
	}
	if crd.Labels[crdmeta.LabelNovaName()] != testNovaName {
		t.Errorf("expected nova name label %s, got %s", testNovaName, crd.Labels[crdmeta.LabelNovaName()])
	}
	if crd.Labels[crdmeta.LabelNovaFlavor()] != testNovaFlavor {
		t.Errorf("expected nova flavor label %s, got %s", testNovaFlavor, crd.Labels[crdmeta.LabelNovaFlavor()])
	}
	if crd.Labels[crdmeta.LabelNovaProject()] != testNovaProject {
		t.Errorf("expected nova project label %s, got %s", testNovaProject, crd.Labels[crdmeta.LabelNovaProject()])
	}
	if crd.Labels[crdmeta.LabelNovaUser()] != testNovaUser {
		t.Errorf("expected nova user label %s, got %s", testNovaUser, crd.Labels[crdmeta.LabelNovaUser()])
	}
}

func TestDomainToHeartbeat(t *testing.T) {
	now := time.Now()
	prev := now.Add(-10 * time.Second)

	domain := &types.DomainState{
		UUID:          "test-uuid",
		Name:          "test-vm",
		Host:          "node001",
		LibvirtDriver: "CH",
		NovaMetadata: types.NovaMetadata{
			Name:    testNovaName,
			Flavor:  testNovaFlavor,
			Project: testNovaProject,
			User:    testNovaUser,
		},
	}

	// First observation
	domain.Status.Update(types.StateRunning, prev, prev, types.SourcePoll)
	domain.XMLHash.Update("hash-v1", prev, prev, types.SourcePoll)

	// Second observation (state changed, hash changed)
	domain.Status.Update(types.StatePaused, now, now, types.SourceEvent)
	domain.XMLHash.Update("hash-v2", now, now, types.SourcePoll)

	heartbeat := DomainToHeartbeat(domain, "10s")

	if heartbeat.Status.Status.Value != "paused" {
		t.Errorf("expected status value paused, got %s", heartbeat.Status.Status.Value)
	}
	if heartbeat.Status.XMLHash.Value != "hash-v2" {
		t.Errorf("expected xmlHash value hash-v2, got %s", heartbeat.Status.XMLHash.Value)
	}

	if heartbeat.Status.Status.PrevValue != "running" {
		t.Errorf("expected status prevValue running, got %s", heartbeat.Status.Status.PrevValue)
	}
	if heartbeat.Status.XMLHash.PrevValue != "hash-v1" {
		t.Errorf("expected xmlHash prevValue hash-v1, got %s", heartbeat.Status.XMLHash.PrevValue)
	}
	if heartbeat.Status.Status.PrevSrc != "poll" {
		t.Errorf("expected status prevSrc poll, got %s", heartbeat.Status.Status.PrevSrc)
	}
	if heartbeat.Status.XMLHash.PrevSrc != "poll" {
		t.Errorf("expected xmlHash prevSrc poll, got %s", heartbeat.Status.XMLHash.PrevSrc)
	}
	if heartbeat.Status.Host != "node001" {
		t.Errorf("expected host node001, got %s", heartbeat.Status.Host)
	}
	if heartbeat.Status.Driver != "CH" {
		t.Errorf("expected driver CH, got %s", heartbeat.Status.Driver)
	}
	if heartbeat.Labels[crdmeta.LabelNovaName()] != testNovaName {
		t.Errorf("expected nova name label %s, got %s", testNovaName, heartbeat.Labels[crdmeta.LabelNovaName()])
	}
	if heartbeat.Labels[crdmeta.LabelNovaFlavor()] != testNovaFlavor {
		t.Errorf("expected nova flavor label %s, got %s", testNovaFlavor, heartbeat.Labels[crdmeta.LabelNovaFlavor()])
	}
	if heartbeat.Labels[crdmeta.LabelNovaProject()] != testNovaProject {
		t.Errorf("expected nova project label %s, got %s", testNovaProject, heartbeat.Labels[crdmeta.LabelNovaProject()])
	}
	if heartbeat.Labels[crdmeta.LabelNovaUser()] != testNovaUser {
		t.Errorf("expected nova user label %s, got %s", testNovaUser, heartbeat.Labels[crdmeta.LabelNovaUser()])
	}
}

func TestNovaMetadataLabelsOmitInvalidValues(t *testing.T) {
	domain := &types.DomainState{
		UUID: "test-uuid",
		Name: "test-vm",
		Host: "node001",
		NovaMetadata: types.NovaMetadata{
			Name:    "valid-name",
			Flavor:  "invalid/value",
			Project: "valid-project",
			User:    "",
		},
	}

	crd := DomainToHeartbeat(domain, "10s")

	if crd.Labels[crdmeta.LabelNovaName()] != "valid-name" {
		t.Errorf("expected valid nova name label, got %q", crd.Labels[crdmeta.LabelNovaName()])
	}
	if _, ok := crd.Labels[crdmeta.LabelNovaFlavor()]; ok {
		t.Error("invalid nova flavor should not be added as label")
	}
	if crd.Labels[crdmeta.LabelNovaProject()] != "valid-project" {
		t.Errorf("expected valid nova project label, got %q", crd.Labels[crdmeta.LabelNovaProject()])
	}
	if _, ok := crd.Labels[crdmeta.LabelNovaUser()]; ok {
		t.Error("empty nova user should not be added as label")
	}
}

// TestCRDCouplingVerificationPattern documents how consumers verify data integrity.
func TestCRDCouplingVerificationPattern(t *testing.T) {
	now := time.Now()

	domain := &types.DomainState{
		UUID: "test-uuid",
		Name: "test-vm",
		Host: "node001",
	}
	domain.Status.Update(types.StateRunning, now, now, types.SourcePoll)
	domain.XMLHash.Update("current-hash", now, now, types.SourcePoll)

	dataCRD := DomainToLibvirtDomain(domain)
	heartbeatCRD := DomainToHeartbeat(domain, "10s")

	dataHash := dataCRD.Status.XMLHash
	heartbeatHash := heartbeatCRD.Status.XMLHash.Value

	if dataHash != heartbeatHash {
		t.Errorf("data CRD hash (%s) should match heartbeat hash (%s)", dataHash, heartbeatHash)
	}

	domain.Status.Update(types.StateGone, now, now, types.SourcePoll)
	deletedAt := now
	domain.DeletedAt = &deletedAt

	dataCRDGone := DomainToLibvirtDomain(domain)
	heartbeatCRDGone := DomainToHeartbeat(domain, "10s")

	if dataCRDGone.Status.XMLHash != heartbeatCRDGone.Status.XMLHash.Value {
		t.Error("hash should still match even when state is gone")
	}

	if heartbeatCRDGone.Status.Status.Value != "gone" {
		t.Errorf("expected heartbeat status to be gone, got %s", heartbeatCRDGone.Status.Status.Value)
	}
}

func TestHeartbeatObservedTime(t *testing.T) {
	statusTime := time.Now()
	xmlTime := statusTime.Add(5 * time.Second)

	domain := &types.DomainState{UUID: "test", Name: "test", Host: "test"}
	domain.Status.Update(types.StateRunning, statusTime, statusTime, types.SourcePoll)
	domain.XMLHash.Update("hash", xmlTime, xmlTime, types.SourcePoll)

	heartbeat := DomainToHeartbeat(domain, "10s")

	observedTime, err := time.Parse(time.RFC3339Nano, heartbeat.Status.Observed)
	if err != nil {
		t.Fatalf("failed to parse observed time: %v", err)
	}

	if observedTime.Before(xmlTime.Add(-time.Second)) {
		t.Errorf("heartbeat Observed should be max of sub-observations, got %v, expected ~%v",
			observedTime, xmlTime)
	}
}

func TestObservationRecordConversion(t *testing.T) {
	now := time.Now()
	prev := now.Add(-10 * time.Second)

	var v types.ObservedValue[types.LibvirtState]
	v.Update(types.StateRunning, prev, prev, types.SourcePoll)
	v.Update(types.StatePaused, now, now, types.SourceEvent)

	rec := observedValueToRecord(v)

	if rec.Value != "paused" {
		t.Errorf("expected Value paused, got %s", rec.Value)
	}
	if rec.PrevValue != "running" {
		t.Errorf("expected PrevValue running, got %s", rec.PrevValue)
	}
	if rec.PrevSrc != "poll" {
		t.Errorf("expected PrevSrc poll, got %s", rec.PrevSrc)
	}
	if rec.Src != "event" {
		t.Errorf("expected Src event, got %s", rec.Src)
	}
	if rec.Queried == "" {
		t.Error("expected Queried to be formatted")
	}
	if rec.PrevQueried == "" {
		t.Error("expected PrevQueried to be formatted")
	}
}

func TestDomainToLibvirtDomainStateReason(t *testing.T) {
	domain := &types.DomainState{
		UUID:            "test-uuid",
		Name:            "test-vm",
		Host:            "node001",
		StateReasonCode: 6, // DomainPausedWatchdog
	}
	domain.Status.Update(types.StatePaused, time.Now(), time.Now(), types.SourcePoll)

	crd := DomainToLibvirtDomain(domain)

	if crd.Status.StateReasonCode != 6 {
		t.Errorf("StateReasonCode = %d, want 6", crd.Status.StateReasonCode)
	}
	if crd.Status.StateReason != "DomainPausedWatchdog" {
		t.Errorf("StateReason = %q, want %q", crd.Status.StateReason, "DomainPausedWatchdog")
	}
	if crd.Status.StateCode != int32(types.StatePaused) {
		t.Errorf("StateCode = %d, want %d", crd.Status.StateCode, int32(types.StatePaused))
	}
}

func TestAddKnownTypesUsesRuntimeSuffix(t *testing.T) {
	originalSuffix := crdmeta.Suffix
	crdmeta.Suffix = "suffix"
	t.Cleanup(func() { crdmeta.Suffix = originalSuffix })

	s := runtime.NewScheme()
	if err := addKnownTypes(s); err != nil {
		t.Fatalf("add known types: %v", err)
	}

	cases := []struct {
		name string
		obj  runtime.Object
		want schema.GroupVersionKind
	}{
		{
			name: "domain",
			obj:  &LibvirtDomain{},
			want: GroupVersion.WithKind("LibvirtDomainSuffix"),
		},
		{
			name: "domain list",
			obj:  &LibvirtDomainList{},
			want: GroupVersion.WithKind("LibvirtDomainSuffixList"),
		},
		{
			name: "heartbeat",
			obj:  &LibvirtDomainHeartbeat{},
			want: GroupVersion.WithKind("LibvirtDomainHeartbeatSuffix"),
		},
		{
			name: "heartbeat list",
			obj:  &LibvirtDomainHeartbeatList{},
			want: GroupVersion.WithKind("LibvirtDomainHeartbeatSuffixList"),
		},
	}

	for _, tc := range cases {
		gvks, _, err := s.ObjectKinds(tc.obj)
		if err != nil {
			t.Fatalf("%s object kinds: %v", tc.name, err)
		}
		if len(gvks) != 1 || gvks[0] != tc.want {
			t.Fatalf("%s GVK = %v, want %v", tc.name, gvks, tc.want)
		}
	}
}
