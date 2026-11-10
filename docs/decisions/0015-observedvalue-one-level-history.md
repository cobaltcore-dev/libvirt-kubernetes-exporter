<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0015 — `ObservedValue[T]`: one level of history per tracked field

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; evidenced by `requirements/DESIGN.md`
and `requirements/REQUIREMENTS.md`.

## Context

Every field we track (libvirt state, XML hash, and future ones like job or pid)
needs to carry not just its value but *when and how* we learned it, so that the
exported record is self-describing enough to reason about freshness
([[0016-time-slice-guarantee]]). We also want the *previous* value and its
timing, because the guarantee we offer spans two consecutive observations and
because consumers doing hash cross-checks need "current or one-back"
([[0017-heartbeat-data-crd-split]]). What we do *not* need is an unbounded audit
trail — this operator exports current truth, not history ([[0011-scope-faithful-export-only]]);
history lives in logs and downstream systems.

## Decision

Represent each tracked field as a generic `ObservedValue[T]` carrying `value`,
`queried`, `observed`, `src`, and exactly one level of history: `prevValue`,
`prevQueried`, `prevObserved`. Crucially `prevValue` is a plain `T`, not a pointer
or a list — so there is exactly one previous value, never a growing chain.

## Consequences

Every field is uniformly self-describing (when queried, when observed, from event
or poll, and what it was just before), which is exactly the substrate the
time-slice window needs. Bounding history to one level means no linked list, no
unbounded growth, and no memory leak — a deliberate ceiling: anyone needing more
than one-step history must go to the logs. The uniform generic also keeps the CRD
status shape regular across fields. The cost is a fixed amount of extra data on
every field even when a consumer only wants the current value.

## Alternatives rejected

- **Bare value with no timing.** Loses the freshness/provenance that is the whole
  point of this exporter.
- **Full history (slice/linked list of past values).** Unbounded growth and a
  leak risk, to serve an audit use-case that belongs in logs, not in a
  current-state CRD.
