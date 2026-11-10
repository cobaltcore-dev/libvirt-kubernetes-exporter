// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package types defines core domain types for the libvirt-kubernetes operator.
package types

import (
	"time"

	"github.com/digitalocean/go-libvirt"
)

// ObservationSource indicates why an observation was made.
type ObservationSource string

const (
	SourcePoll    ObservationSource = "poll"
	SourceEvent   ObservationSource = "event"
	SourceStartup ObservationSource = "startup"
	SourceUnknown ObservationSource = "unknown"
)

// ObservedValue tracks a single observed value with full timestamp history.
// This matches the DESIGN.md spec: value, queried, observed, src, prevValue, prevQueried, prevObserved.
type ObservedValue[T any] struct {
	Value        T                 `json:"value"`
	Queried      time.Time         `json:"queried"`
	Observed     time.Time         `json:"observed"`
	Src          ObservationSource `json:"src"`
	PrevValue    T                 `json:"prevValue,omitempty"`
	PrevSrc      ObservationSource `json:"prevSrc,omitempty"`
	PrevQueried  time.Time         `json:"prevQueried,omitzero"`
	PrevObserved time.Time         `json:"prevObserved,omitzero"`
}

// Update sets a new value, shifting current to prev fields.
func (o *ObservedValue[T]) Update(value T, queried, observed time.Time, src ObservationSource) {
	o.PrevValue = o.Value
	o.PrevSrc = o.Src
	o.PrevQueried = o.Queried
	o.PrevObserved = o.Observed
	o.Value = value
	o.Queried = queried
	o.Observed = observed
	o.Src = src
}

// IsSet returns true if this observation has been set at least once.
func (o *ObservedValue[T]) IsSet() bool {
	return !o.Queried.IsZero()
}

// MigrationObservation holds raw job stats from DomainGetJobStats.
// Only set when jobType != DomainJobNone.
type MigrationObservation struct {
	JobType   libvirt.DomainJobType
	Operation libvirt.DomainJobOperation
}

// Observation is the raw input from libvirt to the state tracker.
type Observation struct {
	// Identity
	UUID          string
	Name          string
	LibvirtDriver string

	// State from DomainGetInfo
	State     LibvirtState
	MaxMem    uint64
	Memory    uint64
	NrVirtCPU uint16
	CPUTime   uint64

	// StateReasonCode from DomainGetState (reason code for the current state)
	StateReasonCode int32

	// Optional: hash of domain XML for change detection
	XMLHash string
	// Optional: raw XML from DomainGetXMLDesc; only set when XMLHash is set
	XML string

	// Timestamps
	Queried  time.Time
	Observed time.Time
	Source   ObservationSource

	// Deletion flag
	IsDeleted bool

	// Migration job stats from DomainGetJobStats; nil when jobType=none
	MigrationInfo *MigrationObservation
}
