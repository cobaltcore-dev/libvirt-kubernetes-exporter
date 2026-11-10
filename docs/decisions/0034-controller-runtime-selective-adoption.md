<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0034 — Selective adoption of controller-runtime for domain writes, drift, and retention

Status: accepted · Date: 2026-09

## Context

Earlier records ([[0021-optimistic-k8s-writes]], [[0027-custom-logging]]) captured
active wariness about controller-runtime: its informer cache hides when it talks
to the network, its logging conventions buried real faults under framework stack
traces in a sibling operator, and adopting it wholesale was explicitly deferred
until its behaviour was understood. At the same time, [[0029-drift-detection]]
noted honestly that drift detection and retention are *classical controller
patterns* — subscribe to changes and reconcile — and that controller-runtime
exists precisely for those cases.

The tension is real but the resolution is not "adopt everything" or "adopt
nothing." The operator has two distinct write paths with very different
requirements:

- **Heartbeat CRDs** are written on every poll cycle. Throughput and latency
  matter. We want to know exactly when and how often they touch the API server.
  A hand-rolled dynamic-client loop with an explicit UID cache gives that control.

- **Domain CRDs, drift detection, and retention** are slow-moving and
  event-driven. A domain CRD changes when VM state changes; drift is corrected
  when it is noticed; a retained CRD is deleted once after a timer. These are
  exactly the cases where controller-runtime's watch-then-reconcile pattern
  eliminates polling, provides built-in requeue/backoff, and handles
  resourceVersion bookkeeping without a hand-rolled cache. The informer cache
  cost is acceptable here because the object set is small and slow-changing.

## Decision

Adopt controller-runtime **selectively**:

- `DomainWriteController`, `DriftDetectionController`, and
  `DeleteRetentionController` run under a `ctrl.Manager` wired into
  `operator.Run`.
- The heartbeat write path (`Exporter.RunHeartbeats`) stays hand-rolled on the
  dynamic client — no controller-runtime involvement.
- Controller-runtime's internal logger is bridged to the slog pipeline via
  `ctrl.SetLogger(logr.FromSlogHandler(...))` so its cache/watch/reconcile output
  flows through the same stream as the rest of the operator ([[0027-custom-logging]]).

The split is enforced structurally: the manager owns domain-CRD objects; the
exporter owns heartbeat objects. They share only the `DomainKnownStore` interface
(UUID → UID mapping) so the heartbeat writer knows when a domain CRD exists and
can set the owner reference.

## Consequences

The operator gains watch-driven reconciliation for the paths that need it —
drift and retention corrections are event-triggered rather than polled — without
paying the throughput cost on the heartbeat path. The previously-open question
about controller-runtime is settled: it is the right tool for slow, event-driven
object management; it is the wrong tool for a high-frequency, latency-sensitive
write loop. The logging bridge ([[0027-custom-logging]]) removes the
observability concern that originally motivated avoiding controller-runtime
entirely — its internal logs are now visible and level-controlled alongside ours.

The cost of the split is the boundary between the two paths: the `DomainKnownStore`
interface must stay consistent, and the heartbeat writer must not start writing for
a UUID until the domain CRD exists (it skips unknown UUIDs). This is a small and
explicit coupling point rather than a hidden one.

## Alternatives rejected

- **Controller-runtime for everything, including heartbeats.** Heartbeat
  throughput is the primary write-rate concern; giving a framework implicit
  control over that path conflicts with the explicit network-behaviour requirement
  of [[0021-optimistic-k8s-writes]].
- **Controller-runtime for nothing; keep hand-rolled loops for domain writes and
  drift too.** The `drift.Detector` ticker was the first attempt at this. It
  worked but required manual LIST + resourceVersion compare, a separate ticker
  goroutine, and a hand-rolled retry discipline — all things controller-runtime
  provides for free on the paths where we do not need fine-grained throughput
  control.
- **Wait until controller-runtime's behaviour is "fully understood."** The
  original lean. Resolved by the logging bridge (its output is now observable)
  and by the realisation that the concern applies specifically to the heartbeat
  path, not to domain CRDs and retention.
