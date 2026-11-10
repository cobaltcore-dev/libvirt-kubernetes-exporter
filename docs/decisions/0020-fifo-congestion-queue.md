<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0020 — FIFO map-coalescing congestion queue (heap ordering dropped for now)

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the "LLM-suggested simplification"
attribution is recollection.

## Context

When Kubernetes writes are slower than state changes, updates back up and we must
coalesce: only the *latest* state per VM matters, and stale intermediate updates
should be dropped rather than replayed. The coalescing itself has always been
map-based — a per-UUID map holding the latest value. The old implementation added
a timestamp-ordered heap *on top of* that map purely to choose which pending VM
to send next; the map still held the values, the heap was a quick-but-complicated
index for "next," and dequeue had to detect and skip heap entries the map had
already superseded. So the heap was an optional refinement of selection order, not
a second source of truth. That refinement was real code to keep in sync, and its
benefit was never actually verified: we had no way, before this rewrite's fakes
and harness, to test whether "oldest-first by timestamp" beat a simpler selection
under congestion.

## Decision

Use a plain FIFO queue: a `map[uuid]*Update` holding the latest update per VM plus
a slice giving arrival order. A new update for a UUID already pending replaces the
map entry (coalescing) without re-queuing; dequeue pops the front of the slice.
The map-based coalescing is unchanged from before; what changed is the "next"
selection — a slice (FIFO) where the old code kept a timestamp heap. Dropping the
heap was an LLM-suggested simplification we accepted because its ordering benefit
was unproven and it can be reintroduced if a scenario shows it matters.

## Consequences

Far less code and no map/heap synchronization, with the same coalescing outcome:
the freshest state per VM wins. We give up guaranteed oldest-first delivery, which
in practice appears fine. Crucially, we now *can* test this: the harness lets us
build congestion scenarios ([[0024-scenario-based-testing]]) and revisit the
decision with evidence instead of intuition. This decision is coupled to how we
write to Kubernetes: for retention and configuration-drift detection we may move
the data CRDs toward a controller-style watch, and an opportunistic write would
then echo back on our own subscription — extra load that is acceptable for the
slow-changing data CRDs but not for heartbeats ([[0021-optimistic-k8s-writes]],
[[0017-heartbeat-data-crd-split]]).

## Alternatives rejected

- **Keep the timestamp heap.** Retained a "next"-selection order whose benefit was
  never measured, at a real synchronization cost; drop it until a scenario
  justifies it.
- **No queue, block the tracker on k8s writes.** Would let a slow API server stall
  observation and defeat coalescing entirely.
