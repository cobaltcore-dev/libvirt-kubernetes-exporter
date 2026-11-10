// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"encoding/json"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"libvirt.org/go/libvirtxml"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

// Re-export crdmeta constants for backward compatibility.
// New code should import crdmeta directly.
const (
	Group   = crdmeta.Group
	Version = crdmeta.Version
)

// Kind name functions (dynamic due to CRD_SUFFIX support).
//
// Deprecated: import crdmeta and use crdmeta.LibvirtDomainKind() directly.
var (
	LibvirtDomainKind          = crdmeta.LibvirtDomainKind
	LibvirtDomainHeartbeatKind = crdmeta.LibvirtDomainHeartbeatKind
)

// Resource name functions (dynamic due to CRD_SUFFIX support).
//
// Deprecated: import crdmeta and use crdmeta.LibvirtDomainResource() directly.
var (
	LibvirtDomainResource          = crdmeta.LibvirtDomainResource
	LibvirtDomainHeartbeatResource = crdmeta.LibvirtDomainHeartbeatResource
)

// HARD-RENAME SYNC POINT (2 of 2): the path= plural below MUST stay identical to
// BaseLibvirtDomainResource in operator/pkg/crdmeta/meta.go. controller-gen reads
// this marker; the operator reads the const — a rename must touch BOTH, then
// re-run `make generate`.
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=libvirtdomains,singular=libvirtdomain,shortName=lvd,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="StateReason",type=string,JSONPath=`.status.stateReason`
// +kubebuilder:printcolumn:name="MigrationJob",type=string,JSONPath=`.status.migration.jobType`
// +kubebuilder:printcolumn:name="MigrationRole",type=string,JSONPath=`.status.migration.role`
// +kubebuilder:printcolumn:name="Flavor",type=string,JSONPath=`.status.novaMetadata.flavor`
// +kubebuilder:printcolumn:name="Nova Name",type=string,JSONPath=`.status.novaMetadata.name`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.status.novaMetadata.project`
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.status.novaMetadata.user`
// +kubebuilder:printcolumn:name="LastChanged",type=date,JSONPath=`.status.lastChanged`,priority=1
// +kubebuilder:printcolumn:name="DeletedAt",type=string,JSONPath=`.status.deletedAt`,priority=1
// +kubebuilder:printcolumn:name="PotentiallyCorrupted",type=boolean,JSONPath=`.status.potentiallyCorrupted`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LibvirtDomain is the Data CRD - holds domain configuration.
// Only updates when definition changes.
type LibvirtDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status LibvirtDomainStatus `json:"status,omitempty"`
}

type LibvirtDomainStatus struct {
	UUID         string                `json:"uuid"`                // libvirt domain UUID
	Name         string                `json:"name"`                // libvirt domain name
	Host         string                `json:"host"`                // hypervisor hostname (from Downward API)
	XMLHash      string                `json:"xmlHash,omitempty"`   // hash of DomainGetXMLDesc; changes when VM config changes
	DomainXML    *apiextensionsv1.JSON `json:"domainXML,omitempty"` // parsed DomainGetXMLDesc as JSON; only set when XMLHash changes
	NovaMetadata NovaMetadataStatus    `json:"novaMetadata,omitempty,omitzero"`
	State        string                `json:"state"`     // human-readable libvirt state (e.g. "running", "shutoff")
	StateCode    int32                 `json:"stateCode"` // raw libvirt state code from DomainGetInfo
	// +kubebuilder:validation:Optional
	StateReason string `json:"stateReason,omitempty"` // human-readable reason from DomainGetState (e.g. "DomainPausedWatchdog")
	// +kubebuilder:validation:Optional
	StateReasonCode      int32  `json:"stateReasonCode,omitempty"`      // raw reason code from DomainGetState
	MaxMemoryKB          int64  `json:"maxMemoryKB,omitempty"`          // configured memory ceiling from DomainGetInfo
	MemoryKB             int64  `json:"memoryKB,omitempty"`             // memory currently in use from DomainGetInfo
	VCPUs                int32  `json:"vcpus,omitempty"`                // number of virtual CPUs from DomainGetInfo
	LastChanged          string `json:"lastChanged,omitempty"`          // timestamp of last state change
	DeletedAt            string `json:"deletedAt,omitempty"`            // set when domain goes missing; retention controller deletes CRD after retention period
	PotentiallyCorrupted bool   `json:"potentiallyCorrupted,omitempty"` // set by drift controller when CRD exists but the operator has no tracked domain
	// +kubebuilder:validation:Optional
	Migration *MigrationStatus `json:"migration,omitempty"` // current migration job; absent when no migration is observed
}

// MigrationStatus is the CRD representation of a domain's migration job.
type MigrationStatus struct {
	Role    string `json:"role"`              // "source" or "destination"
	JobType string `json:"jobType,omitempty"` // e.g. "DomainJobBounded", "DomainJobCompleted"
}

type NovaMetadataStatus struct {
	Name    string `json:"name,omitempty"`
	Flavor  string `json:"flavor,omitempty"`
	Project string `json:"project,omitempty"`
	User    string `json:"user,omitempty"`
}

func (m NovaMetadataStatus) IsZero() bool {
	return m.Name == "" && m.Flavor == "" && m.Project == "" && m.User == ""
}

// +kubebuilder:object:root=true

// LibvirtDomainList is a list of LibvirtDomain resources.
type LibvirtDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []LibvirtDomain `json:"items"`
}

// HARD-RENAME SYNC POINT (2 of 2): the path= plural below MUST stay identical to
// BaseLibvirtDomainHeartbeatResource in operator/pkg/crdmeta/meta.go (see the
// note on the LibvirtDomain struct above). A rename must touch BOTH, then
// re-run `make generate`.
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=libvirtdomainheartbeats,singular=libvirtdomainheartbeat,shortName=lvdhb,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.status.value`
// +kubebuilder:printcolumn:name="Observed",type=date,JSONPath=`.status.observed`
// +kubebuilder:printcolumn:name="PrevState",type=string,JSONPath=`.status.status.prevValue`,priority=1
// +kubebuilder:printcolumn:name="PrevObserved",type=date,JSONPath=`.status.status.prevObserved`,priority=1
// +kubebuilder:printcolumn:name="TTL",type=string,JSONPath=`.status.ttl`,priority=1
// +kubebuilder:printcolumn:name="Flavor",type=string,JSONPath=`.metadata.labels.libvirt\.kvm\.cloud\.sap/nova-flavor`
// +kubebuilder:printcolumn:name="Nova Name",type=string,JSONPath=`.metadata.labels.libvirt\.kvm\.cloud\.sap/nova-name`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.metadata.labels.libvirt\.kvm\.cloud\.sap/nova-project`
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.metadata.labels.libvirt\.kvm\.cloud\.sap/nova-user`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LibvirtDomainHeartbeat is the per-VM Heartbeat CRD.
// Updates frequently with observation timestamps.
type LibvirtDomainHeartbeat struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status HeartbeatStatus `json:"status,omitempty"`
}

type HeartbeatStatus struct {
	TTL      string `json:"ttl"`      // expected update interval; consumers use this to detect a stale operator
	Observed string `json:"observed"` // max of all sub-observation timestamps; latest time anything was seen
	Host     string `json:"host,omitempty"`
	Driver   string `json:"driver,omitempty"`

	// Status tracks the VM state with full prev* history.
	Status ObservationRecord `json:"status"`
	// XMLHash tracks the VM config hash with full prev* history.
	// Consumers can verify the domain CRD is current by comparing XMLHash.value == LibvirtDomain.status.xmlHash.
	XMLHash ObservationRecord `json:"xmlHash,omitempty"`
}

// ObservationRecord is the JSON representation for CRD status.
// Value is context-dependent: a libvirt state string for Status, a hash string for XMLHash.
type ObservationRecord struct {
	Value        string `json:"value"`                  // current observed value
	Queried      string `json:"queried"`                // when the libvirt API was called
	Observed     string `json:"observed"`               // when the value was first seen (may differ from queried for events)
	Src          string `json:"src"`                    // observation source: "poll" or "event"
	PrevValue    string `json:"prevValue,omitempty"`    // value before the last change
	PrevSrc      string `json:"prevSrc,omitempty"`      // source of the previous observation
	PrevQueried  string `json:"prevQueried,omitempty"`  // queried timestamp of the previous observation
	PrevObserved string `json:"prevObserved,omitempty"` // observed timestamp of the previous observation
}

// +kubebuilder:object:root=true

// LibvirtDomainHeartbeatList is a list of LibvirtDomainHeartbeat resources.
type LibvirtDomainHeartbeatList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []LibvirtDomainHeartbeat `json:"items"`
}

// clampUint64ToInt64 converts uint64 to int64, clamping to math.MaxInt64 on overflow.
func clampUint64ToInt64(v uint64) int64 {
	const maxInt64 = 1<<63 - 1
	if v > maxInt64 {
		return maxInt64
	}
	return int64(v)
}

// Helper functions for converting between internal types and CRD types

// DomainToLibvirtDomain converts internal DomainState to LibvirtDomain CRD.
func DomainToLibvirtDomain(d *types.DomainState) *LibvirtDomain {
	status := LibvirtDomainStatus{
		UUID:            d.UUID,
		Name:            d.Name,
		Host:            d.Host,
		XMLHash:         d.XMLHash.Value,
		NovaMetadata:    novaMetadataStatus(d.NovaMetadata),
		State:           d.Status.Value.String(),
		StateCode:       int32(d.Status.Value),
		StateReason:     types.StateReasonString(d.Status.Value, d.StateReasonCode),
		StateReasonCode: d.StateReasonCode,
		MaxMemoryKB:     clampUint64ToInt64(d.MaxMem),
		MemoryKB:        clampUint64ToInt64(d.Memory),
		VCPUs:           int32(d.NrVirtCPU),
		LastChanged:     d.Status.Observed.Format(metav1.RFC3339Micro),
	}
	if d.DeletedAt != nil {
		status.DeletedAt = d.DeletedAt.UTC().Format(metav1.RFC3339Micro)
	}

	if d.Migration != nil {
		status.Migration = migrationStatusToAPI(d.Migration)
	}

	if d.XML != "" {
		var dom libvirtxml.Domain
		if err := dom.Unmarshal(d.XML); err == nil {
			if b, err := json.Marshal(&dom); err == nil {
				status.DomainXML = &apiextensionsv1.JSON{Raw: b}
			}
		}
	}

	return &LibvirtDomain{
		TypeMeta: metav1.TypeMeta{
			APIVersion: crdmeta.APIVersion(),
			Kind:       crdmeta.LibvirtDomainKind(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   d.Host + "-" + d.UUID,
			Labels: domainLabels(d),
		},
		Status: status,
	}
}

// DomainToHeartbeat converts internal DomainState to LibvirtDomainHeartbeat CRD.
func DomainToHeartbeat(d *types.DomainState, ttl string) *LibvirtDomainHeartbeat {
	observed := d.Status.Observed
	if d.XMLHash.Observed.After(observed) {
		observed = d.XMLHash.Observed
	}

	return &LibvirtDomainHeartbeat{
		TypeMeta: metav1.TypeMeta{
			APIVersion: crdmeta.APIVersion(),
			Kind:       crdmeta.LibvirtDomainHeartbeatKind(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   d.Host + "-" + d.UUID,
			Labels: domainLabels(d),
		},
		Status: HeartbeatStatus{
			TTL:      ttl,
			Observed: observed.Format(metav1.RFC3339Micro),
			Host:     d.Host,
			Driver:   d.LibvirtDriver,
			Status:   observedValueToRecord(d.Status),
			XMLHash:  observedValueToRecordString(d.XMLHash),
		},
	}
}

func domainLabels(d *types.DomainState) map[string]string {
	labels := map[string]string{
		crdmeta.LabelHost(): d.Host,
		crdmeta.LabelUUID(): d.UUID,
		crdmeta.LabelName(): d.Name,
	}
	setLabelIfValid(labels, crdmeta.LabelNovaName(), d.NovaMetadata.Name)
	setLabelIfValid(labels, crdmeta.LabelNovaFlavor(), d.NovaMetadata.Flavor)
	setLabelIfValid(labels, crdmeta.LabelNovaProject(), d.NovaMetadata.Project)
	setLabelIfValid(labels, crdmeta.LabelNovaUser(), d.NovaMetadata.User)
	return labels
}

func setLabelIfValid(labels map[string]string, key, value string) {
	if value == "" || len(validation.IsValidLabelValue(value)) > 0 {
		return
	}
	labels[key] = value
}

func novaMetadataStatus(m types.NovaMetadata) NovaMetadataStatus {
	return NovaMetadataStatus{
		Name:    m.Name,
		Flavor:  m.Flavor,
		Project: m.Project,
		User:    m.User,
	}
}

func observedValueToRecord(v types.ObservedValue[types.LibvirtState]) ObservationRecord {
	rec := ObservationRecord{
		Value:    v.Value.String(),
		Queried:  v.Queried.Format(metav1.RFC3339Micro),
		Observed: v.Observed.Format(metav1.RFC3339Micro),
		Src:      string(v.Src),
	}
	if !v.PrevQueried.IsZero() {
		rec.PrevValue = v.PrevValue.String()
		rec.PrevSrc = string(v.PrevSrc)
		rec.PrevQueried = v.PrevQueried.Format(metav1.RFC3339Micro)
		rec.PrevObserved = v.PrevObserved.Format(metav1.RFC3339Micro)
	}
	return rec
}

// DeepCopyInto copies LibvirtDomainStatus, handling pointer fields that
// controller-gen cannot generate (apiextensionsv1.JSON and MigrationStatus).
func (in *LibvirtDomainStatus) DeepCopyInto(out *LibvirtDomainStatus) {
	*out = *in
	if in.DomainXML != nil {
		raw := make([]byte, len(in.DomainXML.Raw))
		copy(raw, in.DomainXML.Raw)
		out.DomainXML = &apiextensionsv1.JSON{Raw: raw}
	}
	if in.Migration != nil {
		m := *in.Migration
		out.Migration = &m
	}
}

func observedValueToRecordString(v types.ObservedValue[string]) ObservationRecord {
	rec := ObservationRecord{
		Value:    v.Value,
		Queried:  v.Queried.Format(metav1.RFC3339Micro),
		Observed: v.Observed.Format(metav1.RFC3339Micro),
		Src:      string(v.Src),
	}
	if !v.PrevQueried.IsZero() {
		rec.PrevValue = v.PrevValue
		rec.PrevSrc = string(v.PrevSrc)
		rec.PrevQueried = v.PrevQueried.Format(metav1.RFC3339Micro)
		rec.PrevObserved = v.PrevObserved.Format(metav1.RFC3339Micro)
	}
	return rec
}

func migrationStatusToAPI(m *types.MigrationStatus) *MigrationStatus {
	if m == nil {
		return nil
	}
	return &MigrationStatus{
		Role:    m.Role,
		JobType: m.JobType.String(),
	}
}
