<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0012 — Observer → Tracker → Exporter pipeline in separate packages

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the rewrite motivation is evidenced by
`requirements/OLD_COMPARE.md` and recollection of the old codebase.

## Context

The first implementation (still running in production) grew organically as we
*discovered* what state needed tracking. It began as one `uuid → DomainState`
map. Then we found that domain state and libvirt XML are two different
observations that cross-pollinate — the heartbeat needed metadata that only lives
in the XML — so a second map appeared. Then, because we also had to remember what
Kubernetes already knew (a cache-like concern), each map doubled again: `states`
vs `statesK8S`, `xmls` vs `xmlsK8S`. Four overlapping representations of "the
state," all in one package, with tracking, k8s-action decisions, and logging
interleaved in the same functions (`processStateMessage` did everything). It
worked and is faithful about domain state, but it was hard to reason about and
hard to extend for the advanced features we now want.

The requirement for the rewrite was therefore not new behaviour first, but a
shape that makes the *existing* behaviour reason-about-able and gives each concern
one home.

## Decision

Structure the operator as a one-directional pipeline of three stages in separate
packages: **Observer** (`pkg/libvirt`) produces `Observation`s from poll+events,
**Tracker** (`pkg/state`) owns the single `domains` map and emits `Update`s,
**Exporter** (`pkg/exporter`) writes CRDs. Types (`Observation` in, `Update` out)
live in `pkg/types` and define the contracts between stages. Data flows one way
via channels: `Observation → Tracker → Update → Exporter`.

## Consequences

Each stage has one responsibility and the compiler enforces the boundaries; the
four-map sprawl collapses to a single authoritative map ([[0013-single-threaded-state-ownership]]),
with "what k8s knows" handled at write time rather than mirrored
([[0021-optimistic-k8s-writes]]). The pipeline shape is also what makes the fakes
and harness tractable — you can drive one stage in isolation
([[0022-fake-libvirt-and-fake-k8s]]). The cost is more packages and explicit
type/channel plumbing than a single struct, and a deliberate loss of the old
code's features (multi-source, missing-detection) that had to be re-added on the
cleaner base rather than carried over.

## Alternatives rejected

- **Iterate on the old monolith.** The dual-then-quadruple maps and
  mode-dependent functions were the actual pain; patching advanced features onto
  them would deepen it.
- **Separate stages but shared mutable state (no channels).** Keeps the coupling
  that made the old code hard to follow; channels make ownership explicit.
