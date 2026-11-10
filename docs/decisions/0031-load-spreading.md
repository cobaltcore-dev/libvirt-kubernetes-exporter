<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0031 — Load spreading: stagger polling, throttle heartbeats, rate-limit writes (over a coalescing base)

Status: partially active · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from code (`pkg/libvirt/observer.go`, `pkg/state/tracker.go`,
`pkg/exporter/exporter.go`, `pkg/exporter/congestion.go`) +
`dev/feat-advanced-notes/{TODO2,IMPL_PLAN,DONE1}.md`. See the status note below —
this is the ADR where inferred-vs-active most matters.

## Context

With many VMs on a host, the naive design spikes the Kubernetes API server: every
poll produces an observation per VM, every observation could become a heartbeat
write, and all of them land at once each interval. But the actual CRD update-rate
limits in our environment are unknown and unmeasured — the same uncertainty that
motivated the fast/slow CRD split ([[0017-heartbeat-data-crd-split]]). So rather
than guess one number, we want independent levers to spread write pressure, each
tunable, that can be turned on as measurements arrive.

The pressure appears at three pipeline stages, so three levers attack it at three
points, over an always-on coalescing base.

## Decision

Four mechanisms, one at each stage:

1. **Coalescing (base, always on).** The `CongestionQueue` keys pending updates by
   UUID and keeps only the latest, dropping superseded writes
   ([[0020-fifo-congestion-queue]]). This is what makes the rest acceptable.
2. **Staggered polling (observer).** Space per-VM `DomainGetInfo`/XML calls by a
   computed delay across the poll interval instead of bursting; auto-raise the
   effective rate if the VM count won't fit, capped by `maxPollRate`.
3. **Heartbeat throttling (tracker).** Emit a no-change heartbeat only every N
   polls, with a per-UUID FNV-hash phase offset so VMs don't all beat on the same
   poll; a real data change resets the counter and emits immediately.
4. **Per-type rate limiting (exporter).** A token-bucket `rate.Limiter` per CRD
   type (domain vs heartbeat) gates writes with `Wait(ctx)`.

Each reads its own config field and self-disables at zero, so they are independent
levers, not a coupled pipeline.

## Consequences

Write pressure can be shaped at the point it arises — when observations are
produced, whether they become writes, and how fast writes hit the API — with the
congestion queue collapsing rapid same-UUID churn underneath. The levers are
deliberately independent so any can be tuned or disabled alone as real
measurements land.

**Status honesty (important).** Only the **congestion queue is wired into the
running operator today.** Staggered polling, heartbeat throttling, and per-type
rate limiting are all implemented and unit-tested, but `pkg/operator/operator.go`
does not thread their config fields into the observer/tracker/exporter
constructors — so at runtime the stagger rate is 0, the heartbeat period is 0
(always emit), and the rate limiters are nil (unlimited). The config *defaults*
enable them, but the wiring does not pass them through. This ADR records the
design and intent; a reader must not assume stages 2–4 are active without
checking the operator wiring. This gap is the natural next step once CRD-rate
measurement (the open question from [[0017-heartbeat-data-crd-split]]) justifies
specific numbers.

## Alternatives rejected

- **A single global write-rate knob.** One number cannot spread load at the point
  it is generated (a burst still forms upstream); staged levers target each source.
- **Atomic batch updates to Kubernetes.** No atomic multi-object update exists in
  the API; individual writes plus coalescing was chosen instead
  ([[0020-fifo-congestion-queue]]).
- **Pick fixed rates now.** The real limits are unmeasured; independent,
  default-off-at-zero levers defer the numbers until they can be measured rather
  than guessed.
