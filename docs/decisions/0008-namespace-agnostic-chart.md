<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0008 — Keep the chart namespace-agnostic (operator target = release namespace)

Status: accepted · Date: 2026-08

## Context

The operator's `--namespace` flag tells it which namespace to write CRDs into.
The chart plumbed this from a separate value, `config.namespace`, defaulting to
`kvm-operator`. During dev-deployment work that value got pinned to the dev
namespace (`libvirt-kubernetes-exporter-shared`) in a shared values file. The
staggered QA diff — the first time the chart rendered against the *production*
namespace — surfaced the bug: the pod ran in namespace
`libvirt-kubernetes-exporter` but was told to write CRDs into
`libvirt-kubernetes-exporter-shared`. Two independent sources of truth for "the
namespace" had diverged.

## Decision

Make the operator's target namespace default to the **release namespace**:
`--namespace={{ .Values.config.namespace | default .Release.Namespace }}`, with
`config.namespace` empty by default (was the misleading `kvm-operator`). An
explicit `config.namespace` still overrides, but the common case needs no value.
The dev values file's hardcoded namespace pin was removed.

## Consequences

The operator writes CRDs into the same namespace it runs in, in both dev and
production, with no per-deployment namespace value to keep in sync. This was
caught precisely because the staggered pipeline diffs the real production
manifest — validating the value of testing the production artifact, not
just the dev one. The override path remains for the rare case that CRDs should
live in a different namespace than the operator pod.

## Alternatives rejected

- **Keep `config.namespace` as a required, separately-set value.** Rejected: two
  sources of truth for one namespace is exactly what caused the divergence; a
  release-namespace default collapses them.
- **Hardcode the namespace per values file.** Rejected: brittle, and it was the
  proximate cause of the bug (the dev value leaked into the prod-shaped render).
