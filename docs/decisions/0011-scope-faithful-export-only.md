<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0011 — Scope boundary: faithful libvirt export only, higher-level reconciliation deferred

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; rationale is partly inferred.

## Context

Observing libvirt faithfully already produces enough hard problems on its own —
socket failures, missed events, restart gaps, congestion toward the API server.
It is tempting to also *resolve* the messy realities that show up in the data:
the same domain UUID defined on more than one host during resize/live-migration,
orphaned definitions left behind by failed operations, and deciding which of
several definitions is the "real" one. In our QA/dev environment these anomalies
are constant; in production they are rarer but real.

Crucially, a separate team is building a `VirtualMachine` CRD and a
placement/scheduling layer (replacing OpenStack's placement service) that is
*also* all-in on Kubernetes and is the natural owner of that reconciliation. If
this operator tried to answer "which host truly owns this VM," it would duplicate
and pre-empt that layer, and couple a low-level observer to scheduling policy.

## Decision

This operator's job stops at a *faithful export of libvirt observations*. It
reports what each host's libvirt says, with timestamps and honest failure states
— including the awkward truth that a UUID is currently defined on two hosts with
differing XML. It does **not** deduplicate across hosts, resolve orphans, or
decide the canonical VM. Those belong to the consuming `VirtualMachine`/placement
layer.

## Consequences

The operator stays small and single-purpose, and its correctness is judged only
against "does this match what `virsh` on that host would say." Because we
deliberately surface multi-host definitions rather than hide them, per-host
naming is required ([[0018-host-uuid-crd-naming]]), and consumers must be
prepared to see the same UUID from multiple hosts. The gap is that no artifact in
*this* repo describes the consuming layer's contract; the boundary is a
convention between teams, so a reader here should treat "someone else resolves
this" as an assumption, not a guarantee.

## Alternatives rejected

- **Resolve multi-host / orphan / canonical-VM here.** Would fold scheduling
  concerns into an observer and duplicate the other team's `VirtualMachine` CRD;
  rejected to keep one clear responsibility per layer.
- **Wait for the consuming layer before exporting anything useful.** The export
  has standalone debugging value today; blocking on the higher layer would strand
  it.
