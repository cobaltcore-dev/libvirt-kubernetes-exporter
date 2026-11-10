<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0013 — Single-threaded state ownership with channel handoff

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; concurrency motivation is recollection.

## Context

The state this operator tracks is touched by several concurrent activities: a
poller, an event listener, and a Kubernetes writer. In the old implementation
the intent was to avoid locks by ownership, but as cross-communication between
those activities grew the code accreted several mutexes (`statesMu`,
`statesK8SMu`, `xmlsMu`, `xmlsK8SMu`) guarding the four maps — with the attendant
lock-ordering and "who holds what" hazards.

The honest driver, from experience here, is that concurrency correctness is a
recurring weakness across our systems: real parallelism is most often gotten subtly wrong, and an incremental build-by-need style does not produce carefully designed parallel code.
So the rewrite optimises for *not being able to get it wrong* over raw
throughput.

## Decision

Exactly one goroutine — `Tracker.Run` — mutates the `domains` map. Every other
stage communicates by sending values over channels; producers never touch the
map. Values crossing channels are deep-copied so a receiver can never observe the
tracker's map mutating under it. A single mutex remains only to let read-only
callers (e.g. status queries) snapshot the map safely.

## Consequences

State mutation is serialized by construction, so whole classes of race — torn
reads, lost updates, lock-order deadlocks — cannot occur, and the code reads as a
sequence of single-threaded transitions. This is the property the whole
harness+scenario effort exists to protect ([[0024-scenario-based-testing]]). The
costs are deep copies on the hot path (acceptable at our VM counts) and that any
genuinely concurrent work (e.g. parallel per-domain libvirt fetches) must happen
*before* the channel, feeding the single writer, never beside it. The single
writer is also what makes the deferred lockstep/stepping idea feasible to reason
about ([[0025-deterministic-stepping-deferred]]).

## Alternatives rejected

- **Fine-grained mutexes over shared maps (the old approach).** Demonstrably
  hazardous here; the lock-ordering burden is exactly what we set out to remove.
- **Lock-free / sharded structures.** More throughput, far more subtlety —
  precisely the direction our track record says to avoid for a system whose value
  is correctness, not speed.
