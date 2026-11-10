<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0035 — Domain write coalescing via pending map, not the CongestionQueue

Status: accepted · Date: 2026-09

## Context

[[0020-fifo-congestion-queue]] describes the `CongestionQueue` used for
coalescing domain updates before writing to Kubernetes: a `map[uuid]*Update`
holding the latest state per VM, plus a FIFO slice for selecting which VM to
write next. With the move to `DomainWriteController` under controller-runtime
([[0034-controller-runtime-selective-adoption]]), the framework's own work queue
takes over dispatch: it receives a request keyed by UUID, deduplicates concurrent
requests for the same key, and calls `Reconcile`. The ordering slice is redundant.

But coalescing of the *payload* is still necessary. The framework deduplicates
requests, not updates — it cannot drop a stale intermediate `Update` in favour of
a newer one.

## Decision

`DomainWriteController` maintains its own `map[string]*Update` (the pending map),
separate from the `CongestionQueue`. On each incoming event, the latest update
overwrites whatever is pending for that UUID (latest-wins), and a reconcile
request is enqueued. `Reconcile` pops and consumes the pending entry; if nothing
is there the request was already handled by a prior call and the reconcile is a
silent no-op. The `CongestionQueue` is retained unchanged for the heartbeat path,
which is not managed by controller-runtime ([[0017-heartbeat-data-crd-split]]).

## Consequences

Domain write coalescing is split from heartbeat coalescing along the same
boundary as the write paths themselves. The pending map guarantees that a
`Reconcile` call either finds the latest state or finds nothing — never a stale
intermediate. The FIFO ordering guarantee from `CongestionQueue` is absent by
design: for domain CRDs only the latest observed state matters, and
controller-runtime's backoff-requeue handles retry ordering on failure.

## Alternatives rejected

- **Feed domain updates through the existing `CongestionQueue`.** The ordering
  slice is dead weight once controller-runtime provides dispatch; the queue's
  blocking dequeue model is also incompatible with controller-runtime's
  event-source interface.
- **No coalescing; let every update become a reconcile.** Rapid state changes
  would queue many calls all writing the same (latest) state. The pending map
  makes coalescing explicit and cheap.
