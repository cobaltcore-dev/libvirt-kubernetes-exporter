<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0025 — Deterministic stepping / full lockstep + fuzzing (deferred)

Status: deferred · Scope: full lockstep execution + fuzzing · Date: 2026-08 ·
Provenance: reconstructed post-hoc on 2026-08-14 from artifacts + recollection;
evidenced by `dev/harness/step/scheduler.go` and the `testing-harness` proposal.

## Context

Scenario testing ([[0024-scenario-based-testing]]) can construct adverse *inputs*,
but the sharpest bugs live in *interleaving*: what happens when the operator's
internal queues congest and stages interleave in a particular order. Reproducing a
specific interleaving by timing alone is flaky. The idea is to run the operator
"one step at a time" behind a gate, releasing steps in a chosen order — which also
yields "coordinates" (the sequence of steps taken) that make a glitch
deterministically replayable, and opens the door to *fuzzing* the interleaving and
replaying any failure the fuzzer finds.

The tension is intrusiveness. Gating *every* meaningful step across all goroutines
touches the whole codebase and risks distorting the very code it tests, and the
*right* way to do it is not yet clear.

## Decision

Defer the whole feature — full lockstep execution and fuzzing on top of it — to a
later point, to be revisited once the code has matured and the right approach is
clearer. A `Scheduler` seam (a `NoOpScheduler` for production and a
`SteppingScheduler` with step points for tests) currently exists in the harness,
likely carried over from an earlier attempt; it is left in place as a *hint* at how
the feature might look, but it is not the committed design. Removing it would be
equally acceptable — the shape of full lockstep is still undecided.

## Consequences

We do not yet have deterministic interleaving control, replay coordinates, or
fuzzing; correctness for now rests on hand-authored scenarios
([[0024-scenario-based-testing]]) and the single-writer design that keeps
interleavings few ([[0013-single-threaded-state-ownership]]). Recording the
decision now fixes the *intent* — this is a deliberate deferral of a known-valuable
hardening technique, not an oversight — and flags the leftover seam as provisional
so a future reader does not mistake it for the settled approach. When revisited,
the single-writer structure should make gating tractable.

## Alternatives rejected

- **Build full lockstep instrumentation now.** High intrusion across every
  goroutine, risk of distorting behaviour, and no agreed design; better to wait
  until the code matures.
- **Commit to the existing seam as the design.** It may be leftover from a prior
  attempt; treating it as the answer would prejudge a decision we have chosen to
  postpone.
