<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0024 — Scenario-based testing of distributed-systems glitches

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the "capture the glitches we discovered"
framing is recollection.

## Context

The point of the rewrite is not new features but *glitch-free behaviour* for the
behaviours we painfully discovered in the first attempt: poll/event races, stale
overwrites, disappearing-and-reappearing VMs during migration, congestion drops.
These are distributed-systems glitches — timing- and interleaving-dependent — and
they are exactly what conventional integration tests reproduce unreliably or not
at all. With fakes we control (libvirt, k8s), we can finally *stage* a specific
adverse interleaving on purpose and assert the operator behaves.

## Decision

Treat named *scenarios* as first-class test artifacts: an integration harness
orchestrates the fake libvirt, fake k8s, and the operator to reproduce a specific
glitch condition (e.g. a live-migration, an event arriving mid-poll, a congested
queue), repeatably. Correctness is defined against these scenarios, and the
library of scenarios grows as we discover or fear new glitches.

## Consequences

Glitches become regression tests instead of war stories — a discovered race can
be pinned down, reproduced, and guarded. The harness is the vehicle for the whole
correctness argument that the single-writer design
([[0013-single-threaded-state-ownership]]) and the coalescing queue
([[0020-fifo-congestion-queue]]) make claims about. This is also the substrate the
deterministic-stepping idea would sharpen from "reproducible by construction of
inputs" to "reproducible by construction of interleaving"
([[0025-deterministic-stepping-deferred]]). The cost is that scenarios are only as
good as our imagination of failure — they cover what we thought to stage, not the
unknown-unknowns, which is part of why fuzzing is attractive later. The
knowledge-transfer motive is real too: scenarios double as teaching material for
colleagues about how these glitches arise ([[0028-tracing-and-timeline-ui]]).

## Alternatives rejected

- **End-to-end integration tests only.** Cannot reliably reproduce timing-
  dependent interleavings; slow; low failure-path coverage
  ([[0022-fake-libvirt-and-fake-k8s]]).
- **Unit tests of components in isolation only.** Miss the cross-component
  interleavings where these glitches actually live.
