// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// +groupName=kvm.cloud.sap
// +versionName=v1alpha1

// Package v1alpha1 contains the CRD type definitions for LibvirtDomain and LibvirtDomainHeartbeat.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
)

var (
	// GroupVersion is the group/version for this API.
	GroupVersion = schema.GroupVersion{Group: "kvm.cloud.sap", Version: "v1alpha1"}

	// SchemeBuilder registers the types in this package.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this package to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypeWithName(GroupVersion.WithKind(crdmeta.LibvirtDomainKind()), &LibvirtDomain{})
	s.AddKnownTypeWithName(GroupVersion.WithKind(crdmeta.LibvirtDomainKind()+"List"), &LibvirtDomainList{})
	s.AddKnownTypeWithName(GroupVersion.WithKind(crdmeta.LibvirtDomainHeartbeatKind()), &LibvirtDomainHeartbeat{})
	s.AddKnownTypeWithName(GroupVersion.WithKind(crdmeta.LibvirtDomainHeartbeatKind()+"List"), &LibvirtDomainHeartbeatList{})
	return nil
}
