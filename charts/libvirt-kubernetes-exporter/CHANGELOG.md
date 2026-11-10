<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Libvirt-Kubernetes-Exporter

## 1.0.0

The operator exports libvirt VM state to Kubernetes custom resources via two cluster-scoped CRDs: `LibvirtDomain` and `LibvirtDomainHeartbeat`.

Features:
- DaemonSet, ServiceAccount, ClusterRole, ClusterRoleBinding
- Namespace managed via `--create-namespace` at install time
- CRDs: `LibvirtDomain` and `LibvirtDomainHeartbeat` (cluster-scoped)
- `LibvirtDomain` status includes: state, stateReason, stateReasonCode, domainXML, migration status, deletedAt, potentiallyCorrupted
- Configmap-based operator configuration (libvirt URI, heartbeat and polling intervals, delete retention)
- `fakeLibvirt.enabled` option for local development (sidecar + emptyDir socket)
