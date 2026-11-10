// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

const domainXMLWithNovaMetadata = `<domain type="kvm">
  <name>instance-8b416d1c</name>
  <metadata>
    <nova:instance xmlns:nova="http://openstack.org/xmlns/libvirt/nova/1.1">
      <nova:name>example-instance</nova:name>
      <nova:flavor name="m1.large" />
      <nova:owner>
        <nova:user uuid="user-uuid">example-user</nova:user>
        <nova:project uuid="project-uuid">example-project</nova:project>
      </nova:owner>
    </nova:instance>
  </metadata>
</domain>`

func TestTrackerCreateDomain(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	obs := types.Observation{
		UUID:      "uuid-001",
		Name:      "test-vm",
		State:     types.StateRunning,
		MaxMem:    2048,
		Memory:    1024,
		NrVirtCPU: 2,
		CPUTime:   100,
		XMLHash:   "abc123",
		Queried:   time.Now(),
		Observed:  time.Now(),
		Source:    types.SourcePoll,
	}

	tracker.processObservation(obs)

	// Check update was emitted
	select {
	case update := <-updateCh:
		if update.Type != types.UpdateCreated {
			t.Errorf("expected UpdateCreated, got %v", update.Type)
		}
		if update.Domain.UUID != "uuid-001" {
			t.Errorf("expected UUID uuid-001, got %s", update.Domain.UUID)
		}
		if update.Domain.Host != "test-host" {
			t.Errorf("expected Host test-host, got %s", update.Domain.Host)
		}
		if update.Domain.Status.Value != types.StateRunning {
			t.Errorf("expected StateRunning, got %v", update.Domain.Status.Value)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected update not received")
	}

	// Check domain is tracked
	d, ok := tracker.GetDomain("uuid-001")
	if !ok {
		t.Fatal("domain not found")
	}
	if d.Name != "test-vm" {
		t.Errorf("expected Name test-vm, got %s", d.Name)
	}
}

func TestTrackerCachesNovaMetadataFromXML(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	tracker.processObservation(types.Observation{
		UUID:          "uuid-001",
		Name:          "test-vm",
		LibvirtDriver: "CH",
		State:         types.StateRunning,
		XMLHash:       "hash-v1",
		XML:           domainXMLWithNovaMetadata,
		Queried:       time.Now(),
		Observed:      time.Now(),
		Source:        types.SourcePoll,
	})

	update := <-updateCh
	if update.Domain.LibvirtDriver != "CH" {
		t.Errorf("LibvirtDriver = %q, want CH", update.Domain.LibvirtDriver)
	}
	if update.Domain.NovaMetadata.Name != "example-instance" {
		t.Errorf("NovaMetadata.Name = %q", update.Domain.NovaMetadata.Name)
	}
	if update.Domain.NovaMetadata.Flavor != "m1.large" {
		t.Errorf("NovaMetadata.Flavor = %q", update.Domain.NovaMetadata.Flavor)
	}
	if update.Domain.NovaMetadata.Project != "example-project" {
		t.Errorf("NovaMetadata.Project = %q", update.Domain.NovaMetadata.Project)
	}
	if update.Domain.NovaMetadata.User != "example-user" {
		t.Errorf("NovaMetadata.User = %q", update.Domain.NovaMetadata.User)
	}
}

func TestTrackerDataChanged(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	// Create domain
	obs1 := types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Memory:   1024,
		Queried:  time.Now(),
		Observed: time.Now(),
		Source:   types.SourcePoll,
	}
	tracker.processObservation(obs1)
	<-updateCh // drain create

	// Update with state change
	obs2 := types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateShutoff, // changed!
		Memory:   1024,
		Queried:  time.Now(),
		Observed: time.Now(),
		Source:   types.SourceEvent,
	}
	tracker.processObservation(obs2)

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateShutoff {
			t.Errorf("expected StateShutoff, got %v", update.Domain.Status.Value)
		}
		// Check prev value was preserved
		if update.Domain.Status.PrevValue != types.StateRunning {
			t.Errorf("expected PrevValue StateRunning, got %v", update.Domain.Status.PrevValue)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected update not received")
	}
}

func TestTrackerHeartbeatOnly(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	// Create domain
	obs1 := types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Memory:   1024,
		Queried:  time.Now(),
		Observed: time.Now(),
		Source:   types.SourcePoll,
	}
	tracker.processObservation(obs1)
	<-updateCh // drain create

	// Same state - should emit heartbeat
	obs2 := types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning, // same
		Memory:   1024,               // same
		Queried:  time.Now().Add(10 * time.Second),
		Observed: time.Now().Add(10 * time.Second),
		Source:   types.SourcePoll,
	}
	tracker.processObservation(obs2)

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateHeartbeat {
			t.Errorf("expected UpdateHeartbeat, got %v", update.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected heartbeat update not received")
	}
}

func TestTrackerDelete(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	// Create domain
	obs1 := types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  time.Now(),
		Observed: time.Now(),
		Source:   types.SourcePoll,
	}
	tracker.processObservation(obs1)
	<-updateCh // drain create

	if tracker.Count() != 1 {
		t.Errorf("expected 1 domain, got %d", tracker.Count())
	}

	// Delete domain
	obs2 := types.Observation{
		UUID:      "uuid-001",
		IsDeleted: true,
		Queried:   time.Now(),
		Observed:  time.Now(),
		Source:    types.SourceEvent,
	}
	tracker.processObservation(obs2)

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateGone {
			t.Errorf("expected StateGone, got %v", update.Domain.Status.Value)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected delete update not received")
	}

	if tracker.Count() != 1 {
		t.Errorf("expected 1 domain (retained as gone), got %d", tracker.Count())
	}
}

func TestTrackerRun(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	obsCh := make(chan types.Observation, 10)
	tracker := NewTracker("test-host", updateCh)

	ctx := t.Context()

	go func() {
		if err := tracker.Run(ctx, obsCh); err != nil && ctx.Err() == nil {
			t.Errorf("tracker.Run error: %v", err)
		}
	}()

	// Send observation through channel
	obsCh <- types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  time.Now(),
		Observed: time.Now(),
		Source:   types.SourcePoll,
	}

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateCreated {
			t.Errorf("expected UpdateCreated, got %v", update.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected update not received")
	}
}

func TestTrackerVersionIncrement(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	// Create two domains
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "vm1", State: types.StateRunning,
		Queried: time.Now(), Observed: time.Now(), Source: types.SourcePoll,
	})
	u1 := <-updateCh

	tracker.processObservation(types.Observation{
		UUID: "uuid-002", Name: "vm2", State: types.StateRunning,
		Queried: time.Now(), Observed: time.Now(), Source: types.SourcePoll,
	})
	u2 := <-updateCh

	if u1.Domain.Version >= u2.Domain.Version {
		t.Errorf("expected version to increase: %d >= %d", u1.Domain.Version, u2.Domain.Version)
	}

	// Update first domain
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "vm1", State: types.StateShutoff,
		Queried: time.Now(), Observed: time.Now(), Source: types.SourceEvent,
	})
	u3 := <-updateCh

	if u2.Domain.Version >= u3.Domain.Version {
		t.Errorf("expected version to increase on update: %d >= %d", u2.Domain.Version, u3.Domain.Version)
	}
}

func TestTrackerEventMuting(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTrackerWithConfig("test-host", updateCh, TrackerConfig{
		DiscardThreshold: 5 * time.Second,
	})

	now := time.Now()

	// Create domain via event
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  now,
		Observed: now,
		Source:   types.SourceEvent,
	})
	<-updateCh // drain create

	// Poll within threshold should be muted
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StatePaused, // different state, but should be muted
		Queried:  now.Add(1 * time.Second),
		Observed: now.Add(1 * time.Second),
		Source:   types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		t.Errorf("expected poll to be muted, got update type=%v", update.Type)
	case <-time.After(50 * time.Millisecond):
		// expected - poll was muted
	}

	// Poll after threshold should NOT be muted
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StatePaused,
		Queried:  now.Add(6 * time.Second),
		Observed: now.Add(6 * time.Second),
		Source:   types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected update after threshold")
	}
}

func TestTrackerEventMutingNeverMutesDelete(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTrackerWithConfig("test-host", updateCh, TrackerConfig{
		DiscardThreshold: 5 * time.Second,
	})

	now := time.Now()

	// Create domain via event
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  now,
		Observed: now,
		Source:   types.SourceEvent,
	})
	<-updateCh // drain create

	// IsDeleted from poll should NEVER be muted (per FRICTION.md)
	tracker.processObservation(types.Observation{
		UUID:      "uuid-001",
		IsDeleted: true,
		Queried:   now.Add(1 * time.Second),
		Observed:  now.Add(1 * time.Second),
		Source:    types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged (StateGone), got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateGone {
			t.Errorf("expected StateGone, got %v", update.Domain.Status.Value)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("IsDeleted should never be muted")
	}
}

func TestTrackerEventMutingDisabled(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	// No DiscardThreshold - muting disabled
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create domain via event
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  now,
		Observed: now,
		Source:   types.SourceEvent,
	})
	<-updateCh // drain create

	// Poll immediately after should NOT be muted (threshold is 0)
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StatePaused,
		Queried:  now.Add(10 * time.Millisecond),
		Observed: now.Add(10 * time.Millisecond),
		Source:   types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("poll should not be muted when threshold is 0")
	}
}

func TestTrackerDeletedVMRetention(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create domain
	tracker.processObservation(types.Observation{
		UUID:     "uuid-001",
		Name:     "test-vm",
		State:    types.StateRunning,
		Queried:  now,
		Observed: now,
		Source:   types.SourcePoll,
	})
	<-updateCh // drain create

	// Delete domain - should transition to StateGone, not be removed
	tracker.processObservation(types.Observation{
		UUID:      "uuid-001",
		IsDeleted: true,
		Queried:   now,
		Observed:  now,
		Source:    types.SourcePoll,
	})

	// Should emit DataChanged (not Deleted) with StateGone
	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateGone {
			t.Errorf("expected StateGone, got %v", update.Domain.Status.Value)
		}
		if update.Domain.DeletedAt == nil {
			t.Error("expected DeletedAt to be set")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected update not received")
	}

	// Domain should still be tracked
	if tracker.Count() != 1 {
		t.Errorf("expected 1 domain still tracked, got %d", tracker.Count())
	}

	d, ok := tracker.GetDomain("uuid-001")
	if !ok {
		t.Error("domain should still be retrievable")
	}
	if !d.IsGone() {
		t.Error("domain should be marked as gone")
	}
}

func TestTrackerDeletedVMReappears(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create domain
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "test-vm", State: types.StateRunning,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	<-updateCh // drain create

	// Delete domain
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", IsDeleted: true,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	<-updateCh // drain gone

	// Domain reappears
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "test-vm", State: types.StateRunning,
		Queried: now.Add(1 * time.Second), Observed: now.Add(1 * time.Second), Source: types.SourcePoll,
	})

	// Should emit update (state changed from gone to running)
	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged, got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateRunning {
			t.Errorf("expected StateRunning, got %v", update.Domain.Status.Value)
		}
		if update.Domain.DeletedAt != nil {
			t.Error("expected DeletedAt to be cleared on reappear")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected update when VM reappears")
	}
}

func TestTrackerNoRetention(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create domain
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "test-vm", State: types.StateRunning,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	<-updateCh // drain create

	// Delete domain — always transitions to StateGone, retention handled by controller.
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", IsDeleted: true,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged (StateGone), got %v", update.Type)
		}
		if update.Domain.Status.Value != types.StateGone {
			t.Errorf("expected StateGone, got %v", update.Domain.Status.Value)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected gone update")
	}

	if tracker.Count() != 1 {
		t.Errorf("expected 1 domain (retained as gone), got %d", tracker.Count())
	}
}

func TestTrackerStateReasonFlowsThrough(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	obs := types.Observation{
		UUID:            "uuid-001",
		Name:            "test-vm",
		State:           types.StatePaused,
		StateReasonCode: 6, // DomainPausedWatchdog
		Queried:         time.Now(),
		Observed:        time.Now(),
		Source:          types.SourcePoll,
	}
	tracker.processObservation(obs)

	update := <-updateCh
	if update.Domain.StateReasonCode != 6 {
		t.Errorf("StateReasonCode = %d, want 6", update.Domain.StateReasonCode)
	}
}

func TestTrackerStateReasonChangeTriggersDataChanged(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create domain: paused with reason 1 (DomainPausedUser)
	tracker.processObservation(types.Observation{
		UUID:            "uuid-001",
		Name:            "test-vm",
		State:           types.StatePaused,
		StateReasonCode: 1, // DomainPausedUser
		Queried:         now,
		Observed:        now,
		Source:          types.SourcePoll,
	})
	<-updateCh // drain create

	// Same state, different reason: DomainPausedWatchdog
	tracker.processObservation(types.Observation{
		UUID:            "uuid-001",
		Name:            "test-vm",
		State:           types.StatePaused,
		StateReasonCode: 6, // DomainPausedWatchdog
		Queried:         now.Add(10 * time.Second),
		Observed:        now.Add(10 * time.Second),
		Source:          types.SourcePoll,
	})

	select {
	case update := <-updateCh:
		if update.Type != types.UpdateDataChanged {
			t.Errorf("expected UpdateDataChanged on reason change, got %v", update.Type)
		}
		if update.Domain.StateReasonCode != 6 {
			t.Errorf("StateReasonCode = %d, want 6", update.Domain.StateReasonCode)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected UpdateDataChanged when StateReason changes")
	}
}

func TestTrackerTrackedUUIDs(t *testing.T) {
	updateCh := make(chan types.Update, 10)
	tracker := NewTracker("test-host", updateCh)

	now := time.Now()

	// Create two domains
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", Name: "vm1", State: types.StateRunning,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	tracker.processObservation(types.Observation{
		UUID: "uuid-002", Name: "vm2", State: types.StateRunning,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	// drain
	<-updateCh
	<-updateCh

	// Delete one
	tracker.processObservation(types.Observation{
		UUID: "uuid-001", IsDeleted: true,
		Queried: now, Observed: now, Source: types.SourcePoll,
	})
	<-updateCh

	// TrackedUUIDs should include BOTH (retained + active)
	tracked := tracker.TrackedUUIDs()
	if len(tracked) != 2 {
		t.Errorf("expected 2 tracked UUIDs, got %d", len(tracked))
	}
	if _, ok := tracked["uuid-001"]; !ok {
		t.Error("uuid-001 should still be tracked during retention")
	}
	if _, ok := tracked["uuid-002"]; !ok {
		t.Error("uuid-002 should be tracked")
	}
}
