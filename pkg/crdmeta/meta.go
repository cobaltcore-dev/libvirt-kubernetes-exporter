// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package crdmeta is the single source of truth for CRD naming.
// It has NO dependency on k8s types so any component can import it cheaply.
package crdmeta

import (
	"os"
	"strings"
	"unicode"
)

const (
	Group   = "kvm.cloud.sap"
	Version = "v1alpha1"

	// Base plurals — the CRD resource names WITHOUT the runtime CRD_SUFFIX.
	//
	// HARD-RENAME SYNC POINT (1 of 2): these two strings MUST stay identical to
	// the `+kubebuilder:resource:path=` markers on the structs in
	// operator/pkg/types/crd.go. controller-gen reads the markers; the operator
	// reads these consts — neither can read the other, so a rename must touch
	// BOTH places, then re-run `make generate`. See the matching note in crd.go.
	//
	// Suffixed with "2" to avoid clashing with the previous implementation's CRDs
	// (libvirtdomains / libvirtdomainxmls).
	BaseLibvirtDomainResource          = "libvirtdomains"
	BaseLibvirtDomainHeartbeatResource = "libvirtdomainheartbeats"

	// Label prefix for domain labels (e.g. "libvirt.kvm.cloud.sap/uuid").
	LabelPrefix = "libvirt." + Group
)

// Suffix is read once from CRD_SUFFIX (empty in production, e.g. "-marcus" for dev).
var Suffix = os.Getenv("CRD_SUFFIX")

// suffixToPascal converts a kebab suffix to PascalCase for use in kind names.
// e.g. "-marcus" -> "Marcus", "-my-env" -> "MyEnv", "" -> ""
func suffixToPascal(s string) string {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "-")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		b.WriteRune(unicode.ToUpper(r[0]))
		b.WriteString(string(r[1:]))
	}
	return b.String()
}

// Runtime-resolved plural resource names (funcs, because Suffix is runtime).
func LibvirtDomainResource() string          { return BaseLibvirtDomainResource + Suffix }
func LibvirtDomainHeartbeatResource() string { return BaseLibvirtDomainHeartbeatResource + Suffix }

// Runtime-resolved kind names (funcs, because Suffix is runtime).
func LibvirtDomainKind() string          { return "LibvirtDomain" + suffixToPascal(Suffix) }
func LibvirtDomainHeartbeatKind() string { return "LibvirtDomainHeartbeat" + suffixToPascal(Suffix) }

// APIVersion returns the full API version string (e.g. "kvm.cloud.sap/v1alpha1").
func APIVersion() string { return Group + "/" + Version }

// Label keys for domain resources.
func LabelHost() string        { return LabelPrefix + "/host" }
func LabelUUID() string        { return LabelPrefix + "/uuid" }
func LabelName() string        { return LabelPrefix + "/name" }
func LabelNovaName() string    { return LabelPrefix + "/nova-name" }
func LabelNovaFlavor() string  { return LabelPrefix + "/nova-flavor" }
func LabelNovaProject() string { return LabelPrefix + "/nova-project" }
func LabelNovaUser() string    { return LabelPrefix + "/nova-user" }

// UUIDFromName extracts the UUID from a CRD name in "$hostname-$uuid" format.
// UUIDs are always 36 characters, so we take the last 36 chars of the name.
// Returns empty string if the name is too short to contain a UUID.
func UUIDFromName(name string) string {
	const uuidLen = 36
	if len(name) < uuidLen {
		return ""
	}
	return name[len(name)-uuidLen:]
}

// HostFromName extracts the hostname from a CRD name in "$hostname-$uuid" format.
// Returns empty string if the name is too short to contain a hostname and UUID.
func HostFromName(name string) string {
	const uuidLen = 36
	// name must be at least "h-<uuid>": 1 char hostname + "-" + 36 char UUID
	if len(name) < uuidLen+2 {
		return ""
	}
	return name[:len(name)-uuidLen-1]
}
