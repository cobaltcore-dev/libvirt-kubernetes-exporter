<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0039 — Project Nova metadata as labels and data status

Status: accepted · Date: 2026-09 · Depends on: 0017, 0032, 0033

## Context

Nova metadata is parsed from libvirt XML because operators need to answer simple
questions with `kubectl`: which VM is this, which flavor/project/user does it
belong to, and can I filter the object list without running the exporter UI or a
custom client? ADR 0033 decided to parse Nova metadata and expose it in the data
CRD, but deliberately left the concrete field layout open. ADR 0017 split the
high-frequency heartbeat CRD from the slower data CRD, so duplicating status data
onto both objects should be deliberate. ADR 0032 covers a broader, still-open idea
of unbounded related-resource UUID labels; this decision is narrower and uses a
fixed key set.

## Decision

Expose the selected Nova fields (`name`, `flavor`, `project`, `user`) as fixed-key
labels on both `LibvirtDomain` and `LibvirtDomainHeartbeat`, using the existing
`libvirt.kvm.cloud.sap/` label prefix. These labels support `kubectl get ... -l`
and readable printer columns on both CRDs.

Expose the complete parsed Nova subset in `LibvirtDomain.status.novaMetadata`.
Do not duplicate that metadata in heartbeat status. The heartbeat CRD remains a
freshness/status object; the data CRD remains the authoritative place for richer
domain metadata.

Only non-empty values that are valid Kubernetes label values are stamped as
labels. The status object still carries the parsed values, including any value
that cannot safely be used as a label.

## Consequences

Users can inspect and filter both CRDs with plain `kubectl` without running the
operator program or another UI. The heartbeat CRD stays compact because Nova
metadata appears only in labels, not in its status payload.

The data CRD carries Nova metadata twice for selectable fields: once as labels for
Kubernetes-native filtering and once in status as the canonical parsed metadata.
This is intentional duplication for two different access patterns.

Label-value validation means some unusual Nova values may be absent from labels
even though they are present in data status. Consumers that need exact metadata
must read `LibvirtDomain.status.novaMetadata`, not infer completeness from
labels.

## Alternatives rejected

- **Put Nova metadata in heartbeat status.** Convenient for direct reads, but it
  grows the high-frequency CRD payload and duplicates data status without helping
  label selectors.
- **Put Nova metadata only in data status.** Keeps objects smaller, but loses the
  `kubectl get ... -l` filtering use case on both CRDs.
- **Use annotations instead of labels.** Avoids label-index concerns, but
  annotations are not selectable and therefore do not support the primary use
  case.
- **Use the ADR 0032 UUID-as-label-key scheme for Nova fields.** That broader
  high-cardinality design remains open. The Nova fields here are a fixed, bounded
  set of label keys and can be decided independently.
