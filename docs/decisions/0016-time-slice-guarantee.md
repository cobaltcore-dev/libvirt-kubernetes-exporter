<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0016 — Time-slice guarantee: `q`/`t` samples and `t0→q1` constant-state windows

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the derivation below is the author's
recollection of the reasoning, evidenced by `requirements/adr.md`.

## Context

The core promise of this operator is faithfulness: when a timestamped observation
appears in Kubernetes, a consumer should be able to trust it as if they had SSH'd
to the host and asked `virsh` themselves ([[0010-export-libvirt-state-to-crds]]).
That promise has to survive the fact that we learn state two different ways — by
*sampling* (polling) and by *transitions* (events) — and we want to treat both in
one representation rather than bolt on special cases.

The derivation follows almost mechanically once framed. A single observation of a value is
really a query with two instants: `q` when we asked and `t` when we got the
answer ([[0015-observedvalue-one-level-history]] carries exactly these). In an
idealised world where we received *every* transition, consecutive observations
would carve the timeline into intervals over which the value is constant. So with
the previous observation's `(q0, t0)` and the current one's `(q1, t1)`, the value
is guaranteed constant across `t0 → q1`: we had the answer at `t0` and had not yet
re-queried at `q1`. Events, having no explicit query, collapse to `q = t` at the
event instant. (The fields were later renamed to `queried`/`observed`.)

## Decision

Track, per observed value, the previous and current query/response instants and
expose the **guaranteed constant-state window as `t0 → q1`** (`prevObserved →
queried`). For event-sourced values, `q = t` at the event time, unifying samples
and transitions under the same window arithmetic.

## Consequences

Consumers get an honest validity interval, not just a point-in-time value, and can
cross-reference libvirt state against other tools with a known window — which is
what makes the export "virsh-grade." Unifying sampling and events in one window
model keeps the record regular and avoids two code paths. The guarantee is
explicitly **not watertight**: it is a sampling approximation (a change *and*
change-back inside a poll gap is invisible, and event leakiness widens the real
uncertainty), so it must be read as "constant as far as we could tell," not a
proof. This honesty is the whole point — outside `t0 → q1` the state is `unknown`,
which is why extended states exist ([[0019-extended-states-and-retention]]).

## Alternatives rejected

- **Report only the latest value with a single timestamp.** Gives a point, not a
  window; consumers cannot bound how long the value has held, defeating
  cross-referencing.
- **Model events and polls with separate schemas.** Two representations to keep
  consistent; the `q = t` collapse lets one window model serve both.
- **Claim a hard guarantee.** Dishonest given sampling gaps and event leakiness;
  we deliberately frame it as a best-effort window.
