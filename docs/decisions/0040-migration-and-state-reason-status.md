<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0040 — Migration and state-reason status via additional API calls

Status: accepted · Date: 2026-10 · Depends on: 0011, 0014, 0017

## Context

Operators need to answer two questions that `DomainGetInfo` alone cannot answer:
why is the VM in its current state (e.g. paused-by-watchdog vs. paused-by-user),
and is a live migration in progress? Both answers require additional libvirt API
calls per domain per observation cycle.

ADR 0011 established that the operator is a faithful exporter, not a
controller — it exports observed state, not aggregated statistics. This is the
tension: `DomainGetJobStats` returns rich migration progress counters
(bytes transferred, bandwidth, memory dirty rate, …), but exporting those as CRD
fields would conflict with ADR 0011's scope and with ADR 0017's design goal of
keeping the data CRD as a state snapshot rather than a metrics endpoint. Progress
counters are high-frequency, volatile, and better served by a dedicated metrics
pipeline.

## Decision

Add two unconditional extra calls per observation (poll and event):

- **`DomainGetState`** — to retrieve `stateReasonCode`, the integer reason code
  for the domain's current libvirt state. Exposed as `status.stateReasonCode` and
  its human-readable form as `status.stateReason` on `LibvirtDomain`.

- **`DomainGetJobStats`** — to detect whether a migration job is active. Only the
  job type (`jobType`) and operation direction (`operation` typed-param) are read.
  When `jobType == DomainJobNone` the field is absent from the CRD. Otherwise
  `status.migration` is set with `jobType` (e.g. `DomainJobBounded`,
  `DomainJobCompleted`) and `role` (`source`, `destination`, or `unknown`),
  derived from the operation direction. `unknown` is used when the `operation`
  typed-param is absent (e.g. at job completion on some libvirt versions). All
  progress counters returned by `DomainGetJobStats` are discarded.

Both calls are unconditional: every poll and every event observation fetches them
regardless of current state. This keeps the observer simple (no gating logic) and
consistent with the approach used for `DomainGetXMLDesc`.

## Consequences

Each observation cycle issues two extra libvirt RPC calls per domain.

The migration field is a point-in-time snapshot, not a progress stream. Consumers
that need transfer statistics should use a dedicated metrics source (e.g. libvirt
exporter for Prometheus), not this CRD.

## Alternatives rejected

- **Export full `DomainGetJobStats` counters to CRD status.** Conflicts with ADR
  0011 (faithful state export, not metrics) and ADR 0017 (data CRD as snapshot).
  Counter fields change every poll during migration, inflating CRD update
  frequency and pushing metrics concerns into the exporter.

- **Gate the extra calls on current state** (e.g. only call `DomainGetJobStats`
  when the domain is running). Adds conditional logic to the observer, makes
  observation paths inconsistent, and risks missing state transitions that occur
  between polls.

- **Expose migration via a separate CRD.** Unnecessary complexity for a
  point-in-time flag. The data CRD is the right home for derived domain state.
  Potentially will be revisited later.
