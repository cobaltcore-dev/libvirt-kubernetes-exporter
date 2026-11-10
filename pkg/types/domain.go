// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"time"

	"github.com/digitalocean/go-libvirt"
)

// LibvirtState represents libvirt domain states.
type LibvirtState uint8

const (
	StateNostate     LibvirtState = 0
	StateRunning     LibvirtState = 1
	StateBlocked     LibvirtState = 2
	StatePaused      LibvirtState = 3
	StateShutdown    LibvirtState = 4
	StateShutoff     LibvirtState = 5
	StateCrashed     LibvirtState = 6
	StatePMSuspended LibvirtState = 7

	// Extended states (synthetic, not from libvirt)
	StateUnknown LibvirtState = 250 // socket failure, cannot determine state
	StateHanging LibvirtState = 251 // operation timeout (future: connection pooling)
	StateGone    LibvirtState = 252 // VM was observed but now missing from poll

	StateDeleted LibvirtState = 255 // synthetic state for removed domains (deprecated, use StateGone)
)

func (s LibvirtState) String() string {
	switch s {
	case StateNostate:
		return "nostate"
	case StateRunning:
		return "running"
	case StateBlocked:
		return "blocked"
	case StatePaused:
		return "paused"
	case StateShutdown:
		return "shutdown"
	case StateShutoff:
		return "shutoff"
	case StateCrashed:
		return "crashed"
	case StatePMSuspended:
		return "pmsuspended"
	case StateUnknown:
		return "unknown"
	case StateHanging:
		return "hanging"
	case StateGone:
		return "gone"
	case StateDeleted:
		return "deleted"
	default:
		return "invalid"
	}
}

// IsExtended returns true if this is a synthetic extended state (not from libvirt).
func (s LibvirtState) IsExtended() bool {
	return s >= 250
}

// StateReasonString returns the reason for the current state, derived from go-libvirt's
// stringer-generated types (e.g. "DomainPausedWatchdog", "DomainRunningMigrated").
// Returns a numeric string for extended/synthetic states.
func StateReasonString(state LibvirtState, reason int32) string {
	switch state {
	case StateNostate:
		return libvirt.DomainNostateReason(reason).String()
	case StateRunning:
		return libvirt.DomainRunningReason(reason).String()
	case StateBlocked:
		return libvirt.DomainBlockedReason(reason).String()
	case StatePaused:
		return libvirt.DomainPausedReason(reason).String()
	case StateShutdown:
		return libvirt.DomainShutdownReason(reason).String()
	case StateShutoff:
		return libvirt.DomainShutoffReason(reason).String()
	case StateCrashed:
		return libvirt.DomainCrashedReason(reason).String()
	case StatePMSuspended:
		return libvirt.DomainPMSuspendedReason(reason).String()
	default:
		return "" // extended/synthetic states (StateGone, StateUnknown, etc.) have no libvirt reason enum
	}
}

// MigrationStatus holds the current migration job status for a domain.
type MigrationStatus struct {
	// "source" or "destination" — source inferred from Operation=MigrationOut, destination otherwise
	Role string `json:"role"`

	JobType libvirt.DomainJobType `json:"jobType,omitempty"`
}

// Equal returns true if both MigrationStatus values are semantically identical,
// dereferencing pointer fields for value comparison.
func (m *MigrationStatus) Equal(other *MigrationStatus) bool {
	if m == nil && other == nil {
		return true
	}
	if m == nil || other == nil {
		return false
	}
	if m.Role != other.Role || m.JobType != other.JobType {
		return false
	}
	return true
}

// MigrationObservationToStatus converts a raw MigrationObservation to MigrationStatus.
// Returns nil if obs is nil.
func MigrationObservationToStatus(obs *MigrationObservation) *MigrationStatus {
	if obs == nil {
		return nil
	}

	var role string
	switch obs.Operation {
	case libvirt.DomainJobOperationStrMigrationOut:
		role = "source"
	case libvirt.DomainJobOperationStrMigrationIn:
		role = "destination"
	default:
		role = "unknown"
	}

	ms := &MigrationStatus{
		Role:    role,
		JobType: obs.JobType,
	}

	return ms
}

// DomainState represents the full observed state of a VM domain.
type DomainState struct {
	// Identity (immutable)
	UUID          string       `json:"uuid"`
	Name          string       `json:"name"`
	Host          string       `json:"host"`                    // hostname where this domain runs
	LibvirtDriver string       `json:"libvirtDriver,omitempty"` // e.g. CH or QEMU
	NovaMetadata  NovaMetadata `json:"novaMetadata,omitzero"`

	// Observed fields with full timestamp tracking
	Status  ObservedValue[LibvirtState] `json:"status"`
	XMLHash ObservedValue[string]       `json:"xmlHash"`

	// Full domain XML from DomainGetXMLDesc; updated only when XMLHash changes
	XML string `json:"xml,omitempty"`

	// Resource info (from DomainGetInfo) - updated on each observation
	MaxMem    uint64 `json:"maxMemKB"`
	Memory    uint64 `json:"memoryKB"`
	NrVirtCPU uint16 `json:"vcpus"`
	CPUTime   uint64 `json:"cpuTimeNs"`

	// StateReasonCode from DomainGetState (reason code for the current state, e.g. DomainPausedWatchdog)
	StateReasonCode int32 `json:"stateReasonCode"`

	// Migration status from DomainGetJobStats; nil when no job is active or recent
	Migration *MigrationStatus `json:"migration,omitempty"`

	// Internal tracking
	FirstSeen time.Time  `json:"firstSeen"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"` // set when VM goes missing, cleared if it reappears
	Version   uint64     `json:"version"`             // monotonically increasing for stale detection
}

// DeepCopy creates a deep copy of the domain state.
func (d *DomainState) DeepCopy() *DomainState {
	if d == nil {
		return nil
	}
	result := &DomainState{
		UUID:            d.UUID,
		Name:            d.Name,
		Host:            d.Host,
		LibvirtDriver:   d.LibvirtDriver,
		NovaMetadata:    d.NovaMetadata,
		Status:          d.Status,
		XMLHash:         d.XMLHash,
		XML:             d.XML,
		MaxMem:          d.MaxMem,
		Memory:          d.Memory,
		NrVirtCPU:       d.NrVirtCPU,
		CPUTime:         d.CPUTime,
		StateReasonCode: d.StateReasonCode,
		FirstSeen:       d.FirstSeen,
		Version:         d.Version,
	}
	if d.Migration != nil {
		m := *d.Migration
		result.Migration = &m
	}
	if d.DeletedAt != nil {
		t := *d.DeletedAt
		result.DeletedAt = &t
	}
	return result
}

// NovaMetadata is the small, operator-facing subset of Nova's libvirt metadata.
type NovaMetadata struct {
	Name    string `json:"name,omitempty"`
	Flavor  string `json:"flavor,omitempty"`
	Project string `json:"project,omitempty"`
	User    string `json:"user,omitempty"`
}

func (m NovaMetadata) IsZero() bool {
	return m.Name == "" && m.Flavor == "" && m.Project == "" && m.User == ""
}

// IsGone returns true if this domain has been marked as gone (missing from poll).
func (d *DomainState) IsGone() bool {
	return d.DeletedAt != nil
}

// UpdateType indicates what kind of change occurred.
type UpdateType int

const (
	UpdateCreated     UpdateType = iota // new domain discovered
	UpdateDataChanged                   // state or config changed
	UpdateHeartbeat                     // only timestamps changed (for heartbeat CRD)
)

func (t UpdateType) String() string {
	switch t {
	case UpdateCreated:
		return "created"
	case UpdateDataChanged:
		return "data_changed"
	case UpdateHeartbeat:
		return "heartbeat"
	default:
		return "unknown"
	}
}

// Update represents a state change to be exported.
type Update struct {
	Domain    *DomainState
	Type      UpdateType
	Timestamp time.Time
}

// FormatUUID formats a 16-byte UUID as a standard UUID string.
func FormatUUID(uuid []byte) string {
	if len(uuid) != 16 {
		return ""
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
