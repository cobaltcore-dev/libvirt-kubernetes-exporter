// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"
	"time"

	"github.com/digitalocean/go-libvirt"
)

func TestObservedValueUpdate(t *testing.T) {
	var v ObservedValue[LibvirtState]

	// First update
	t1 := time.Now()
	t2 := t1.Add(10 * time.Millisecond)
	v.Update(StateRunning, t1, t2, SourcePoll)

	if v.Value != StateRunning {
		t.Errorf("expected StateRunning, got %v", v.Value)
	}
	if v.Src != SourcePoll {
		t.Errorf("expected SourcePoll, got %v", v.Src)
	}
	if !v.PrevQueried.IsZero() {
		t.Error("expected PrevQueried to be zero on first update")
	}

	// Second update - should rotate values
	t3 := t2.Add(10 * time.Second)
	t4 := t3.Add(10 * time.Millisecond)
	v.Update(StateShutoff, t3, t4, SourceEvent)

	if v.Value != StateShutoff {
		t.Errorf("expected StateShutoff, got %v", v.Value)
	}
	if v.PrevValue != StateRunning {
		t.Errorf("expected PrevValue to be StateRunning, got %v", v.PrevValue)
	}
	if v.PrevSrc != SourcePoll {
		t.Errorf("expected PrevSrc to be SourcePoll, got %v", v.PrevSrc)
	}
	if v.PrevQueried != t1 {
		t.Errorf("expected PrevQueried to be t1, got %v", v.PrevQueried)
	}
	if v.Src != SourceEvent {
		t.Errorf("expected SourceEvent, got %v", v.Src)
	}
}

func TestObservedValueIsSet(t *testing.T) {
	var v ObservedValue[string]
	if v.IsSet() {
		t.Error("expected IsSet to be false for zero value")
	}

	v.Update("test", time.Now(), time.Now(), SourcePoll)
	if !v.IsSet() {
		t.Error("expected IsSet to be true after update")
	}
}

func TestLibvirtStateString(t *testing.T) {
	tests := []struct {
		state    LibvirtState
		expected string
	}{
		{StateNostate, "nostate"},
		{StateRunning, "running"},
		{StateBlocked, "blocked"},
		{StatePaused, "paused"},
		{StateShutdown, "shutdown"},
		{StateShutoff, "shutoff"},
		{StateCrashed, "crashed"},
		{StatePMSuspended, "pmsuspended"},
		// Extended states
		{StateUnknown, "unknown"},
		{StateHanging, "hanging"},
		{StateGone, "gone"},
		{StateDeleted, "deleted"},
		// Invalid state returns "invalid" (not "unknown" which is a valid state now)
		{LibvirtState(99), "invalid"},
	}

	for _, tc := range tests {
		if got := tc.state.String(); got != tc.expected {
			t.Errorf("LibvirtState(%d).String() = %q, want %q", tc.state, got, tc.expected)
		}
	}
}

func TestDomainStateDeepCopy(t *testing.T) {
	original := &DomainState{
		UUID:          "test-uuid",
		Name:          "test-vm",
		Host:          "node001",
		LibvirtDriver: "CH",
		NovaMetadata: NovaMetadata{
			Name:   "nova-vm",
			Flavor: "test-flavor",
		},
		MaxMem:    2048,
		Memory:    1024,
		NrVirtCPU: 2,
		CPUTime:   123456,
		FirstSeen: time.Now(),
		Version:   5,
	}
	original.Status.Update(StateRunning, time.Now(), time.Now(), SourcePoll)

	dst := original.DeepCopy()

	// Verify values are equal
	if dst.UUID != original.UUID {
		t.Error("UUID not copied")
	}
	if dst.Version != original.Version {
		t.Error("Version not copied")
	}
	if dst.Status.Value != original.Status.Value {
		t.Error("Status.Value not copied")
	}
	if dst.LibvirtDriver != original.LibvirtDriver {
		t.Error("LibvirtDriver not copied")
	}
	if dst.NovaMetadata != original.NovaMetadata {
		t.Error("NovaMetadata not copied")
	}

	// Verify independence - changing dst shouldn't affect original
	dst.Version = 99
	dst.Name = "changed"
	if original.Version == 99 {
		t.Error("modifying copy changed original Version")
	}
	if original.Name == "changed" {
		t.Error("modifying copy changed original Name")
	}
}

func TestUpdateTypeString(t *testing.T) {
	tests := []struct {
		ut       UpdateType
		expected string
	}{
		{UpdateCreated, "created"},
		{UpdateDataChanged, "data_changed"},
		{UpdateHeartbeat, "heartbeat"},
		{UpdateType(99), "unknown"},
	}

	for _, tc := range tests {
		if got := tc.ut.String(); got != tc.expected {
			t.Errorf("UpdateType(%d).String() = %q, want %q", tc.ut, got, tc.expected)
		}
	}
}

func TestLibvirtStateIsExtended(t *testing.T) {
	tests := []struct {
		state      LibvirtState
		isExtended bool
	}{
		{StateNostate, false},
		{StateRunning, false},
		{StatePaused, false},
		{StateShutoff, false},
		{StateCrashed, false},
		{StatePMSuspended, false},
		// Extended states (250+)
		{StateUnknown, true},
		{StateHanging, true},
		{StateGone, true},
		{StateDeleted, true},
	}

	for _, tc := range tests {
		if got := tc.state.IsExtended(); got != tc.isExtended {
			t.Errorf("LibvirtState(%d).IsExtended() = %v, want %v", tc.state, got, tc.isExtended)
		}
	}
}

func TestMigrationObservationToStatus_Nil(t *testing.T) {
	if got := MigrationObservationToStatus(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

func TestMigrationObservationToStatus_ActiveJob(t *testing.T) {
	obs := &MigrationObservation{
		JobType:   libvirt.DomainJobBounded,
		Operation: libvirt.DomainJobOperationStrMigrationOut,
	}

	ms := MigrationObservationToStatus(obs)
	if ms == nil {
		t.Fatal("expected non-nil MigrationStatus")
	}
	if ms.Role != "source" {
		t.Errorf("expected role=source, got %q", ms.Role)
	}
	if ms.JobType != libvirt.DomainJobBounded {
		t.Errorf("expected JobType=DomainJobBounded, got %v", ms.JobType)
	}
}

func TestMigrationObservationToStatus_DestRole(t *testing.T) {
	obs := &MigrationObservation{
		JobType:   libvirt.DomainJobBounded,
		Operation: libvirt.DomainJobOperationStrMigrationIn,
	}
	ms := MigrationObservationToStatus(obs)
	if ms.Role != "destination" {
		t.Errorf("expected role=destination, got %q", ms.Role)
	}
}

func TestMigrationObservationToStatus_UnknownRole(t *testing.T) {
	obs := &MigrationObservation{
		JobType:   libvirt.DomainJobCompleted,
		Operation: libvirt.DomainJobOperation(0), // operation param absent → unknown
	}
	ms := MigrationObservationToStatus(obs)
	if ms.Role != "unknown" {
		t.Errorf("expected role=unknown, got %q", ms.Role)
	}
}

func TestMigrationStatusEqual_BothNil(t *testing.T) {
	var a, b *MigrationStatus
	if !a.Equal(b) {
		t.Error("expected nil == nil")
	}
}

func TestMigrationStatusEqual_OneNil(t *testing.T) {
	a := &MigrationStatus{Role: "source", JobType: libvirt.DomainJobBounded}
	if a.Equal(nil) {
		t.Error("expected non-nil != nil")
	}
	if (*MigrationStatus)(nil).Equal(a) {
		t.Error("expected nil != non-nil")
	}
}

func TestMigrationStatusEqual_DifferentJobType(t *testing.T) {
	a := &MigrationStatus{Role: "source", JobType: libvirt.DomainJobBounded}
	b := &MigrationStatus{Role: "source", JobType: libvirt.DomainJobCompleted}
	if a.Equal(b) {
		t.Error("expected unequal when JobType differs")
	}
}

func TestDomainStateDeepCopy_WithMigration(t *testing.T) {
	original := &DomainState{
		UUID: "test-uuid",
		Migration: &MigrationStatus{
			Role:    "source",
			JobType: libvirt.DomainJobBounded,
		},
	}

	copied := original.DeepCopy()

	if copied.Migration == nil {
		t.Fatal("Migration not copied")
	}
	if copied.Migration == original.Migration {
		t.Error("Migration pointer not deep-copied")
	}
}

func TestDomainStateIsGone(t *testing.T) {
	d := &DomainState{UUID: "test"}
	if d.IsGone() {
		t.Error("expected IsGone to be false for domain without DeletedAt")
	}

	now := time.Now()
	d.DeletedAt = &now
	if !d.IsGone() {
		t.Error("expected IsGone to be true for domain with DeletedAt")
	}
}

func TestDomainStateDeepCopyWithDeletedAt(t *testing.T) {
	now := time.Now()
	original := &DomainState{
		UUID:      "test-uuid",
		Name:      "test-vm",
		DeletedAt: &now,
	}

	copied := original.DeepCopy()

	// Verify DeletedAt is copied
	if copied.DeletedAt == nil {
		t.Fatal("DeletedAt not copied")
	}
	if *copied.DeletedAt != now {
		t.Error("DeletedAt value not equal")
	}

	// Verify independence - changing copy shouldn't affect original
	later := now.Add(time.Hour)
	copied.DeletedAt = &later
	if original.DeletedAt.Equal(later) {
		t.Error("modifying copy changed original DeletedAt")
	}
}
