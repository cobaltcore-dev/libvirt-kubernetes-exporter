<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0005 — Isolate cluster-scoped resources by release namespace

Status: accepted · Date: 2026-08 · Depends on: 0004, 0009

## Context

With configurable per-branch dev deployments plus production on one cluster
(0009), namespaced objects (DaemonSet, ServiceAccount) isolate for free — the
same name in different namespaces is distinct. But the chart also ships a
**ClusterRole** and **ClusterRoleBinding**, which are cluster-scoped and were
named off the release name (`libvirt-kubernetes-exporter[-admin]`). Two releases
on the same cluster would therefore collide on these global names: Helm would
report ownership conflicts, and uninstalling one would delete the RBAC out from
under the others. (The CRDs are the third cluster-scoped kind, already isolated
by their suffix in 0004.)

## Decision

Name the ClusterRole and ClusterRoleBinding via a `clusterScopedName` helper
keyed on `.Release.Namespace`, which is unique per deployment and already carries
the app name (`libvirt-kubernetes-exporter-shared`, or plain
`libvirt-kubernetes-exporter` for production). The binding's `roleRef` uses the
same helper so it still points at its own role.

## Consequences

Each deployment owns distinct cluster-scoped RBAC; production keeps its original
name (no churn on the existing release). The operator needs **no code change**:
it authenticates via its ServiceAccount token (`rest.InClusterConfig()`) and RBAC
is resolved server-side by the ClusterRoleBinding — the operator never references
the ClusterRole/Binding names, only the CRD resource names (which 0004 handles).

## Alternatives rejected

- **Prefix the name with `fullname` + namespace.** Produced ugly, doubled names
  (`libvirt-kubernetes-exporter-libvirt-kubernetes-exporter-shared`) that flirted
  with the 63-char limit; keying on the namespace alone is cleaner and already
  unique.
- **Leave cluster-scoped names shared.** Rejected: silent cross-deployment
  ownership conflicts and teardown footguns — exactly what multi-tenancy on one
  cluster must avoid.
