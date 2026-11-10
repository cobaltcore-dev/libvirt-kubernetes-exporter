<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0030 — Conflict detection with ownership annotation + immediate takeover, not leader election

Status: accepted (implemented in the exporter write path; **end-to-end tests
absent**) · Date: 2026-08 · Provenance: reconstructed post-hoc on 2026-08-14 from
code (`pkg/conflict/tracker.go`, `pkg/exporter/exporter.go`) +
`dev/feat-advanced-notes/TODO2.md`; the "no leader election" rationale is
evidenced by TODO2.md §B2.

## Context

More than one operator instance could end up writing the same CRD — during a
deployment overlap, a stray dev deployment ([[0009-configurable-dev-deployments]]),
or plain misconfiguration. We need to notice that and react. But two writers for
the same `$host-$uuid` object is, in a correctly-configured cluster, *impossible*:
the per-host naming ([[0018-host-uuid-crd-naming]]) already guarantees a single
legitimate writer per name. So a second writer is not a state to arbitrate — it is
a **misconfiguration to surface**.

That reframes the problem away from consensus. Leader election would add
coordination machinery to arbitrate a situation that should never legitimately
occur, and could itself be misconfigured. What we actually want is to keep
exporting our own truth, detect the anomaly, and shout about it. The catch is
*identity*: the anomaly we most expect is two operators on the **same host**, so
whatever we stamp as "owner" must distinguish two same-host instances — a
hostname cannot.

## Decision

No leader election. Each operator generates a **random per-instance id at
startup** and stamps it as the ownership annotation
`operator.kvm.cloud.sap/owner` on writes to the **heartbeat CRD** (where we want
this detectability). Before updating an existing object it reads that annotation;
if a *different* instance id holds it, the operator **alerts
(`multiple_operators`) but still proceeds** — immediate takeover, because our
naming says we are the legitimate writer. Separately, repeated write conflicts
(HTTP 409) are counted per UUID; at a threshold (default 15) the operator alerts
(`conflict_threshold`) and backs off for a window (default 5m) before retrying, so
genuine transient 409s self-heal without a permanent stall.

Why a per-instance id and not the hostname: the failure we most need to catch is
**two misconfigured operators on the same host**, which would stamp the *same*
hostname and be mutually invisible. A random id per running instance makes
repeated conflicts against *different* instances observable — which is the whole
point of the annotation. (The current code stamps the hostname; switching it to a
startup-generated instance id is the identified correction.)

## Consequences

Multi-operator situations become loud, diagnosable alerts rather than silent
races or a consensus dance, and the operator never stops exporting its own
observations. Backoff-then-retry keeps transient conflicts from becoming alert
storms. The trade-offs and gaps to be honest about: takeover means the ownership
check's result is intentionally ignored at the call site (we always overwrite);
`isConflictError` currently matches on the error *string* rather than
`apierrors.IsConflict`, which is brittle; there are **no unit tests for
`pkg/conflict` and no end-to-end conflict test** — the fake-k8s `InjectConflict`
hook exists but is not wired into its update handler
([[0022-fake-libvirt-and-fake-k8s]]), so the 409 path is unexercised. The
mechanism is wired into the exporter write path, but its verification is
incomplete.

## Alternatives rejected

- **Leader election / lease.** Adds coordination to arbitrate a case that
  correct configuration makes impossible, and can itself be misconfigured; we
  want detection + alert, not silent arbitration.
- **Yield on foreign ownership (back off and stop writing).** Would let a
  misconfigured second operator suppress the legitimate one; instead we take over
  and alert, trusting the `$host-$uuid` naming as the real exclusivity guarantee.
- **Permanent stop after N conflicts.** Turns a transient 409 burst into an
  outage; a time-boxed backoff self-heals.
