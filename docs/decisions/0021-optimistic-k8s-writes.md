<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0021 — Optimistic Kubernetes writes with cached resourceVersion

Status: accepted · Date: 2026-08 · Updated: 2026-09 · Provenance: reconstructed
post-hoc on 2026-08-14 from artifacts + recollection; the "what broke in the old
code" account is recollection, corrected in review. Updated 2026-09: the
conditional "for data CRDs a runtime may provide this" is now resolved — see
Decision and Consequences.

## Context

Writing observed state to Kubernetes has to cope with two things at once: the CRD
may or may not already exist, and its stored content may have drifted from what we
last wrote (a genuine configuration change, or a stale object from before a
restart). The old implementation hand-crafted an opportunistic update procedure
with a single retry and a consistency check across the linked domain and XML
CRDs; on failure it gave up and let the next poll try again. It works for domain
state, but the XML was not reliably refreshed after a migration — not because of
how resourceVersion was handled (that caching worked fine), but because on an
ownership handover the code did not detect that the incoming observation was
*new* and warranted a rewrite. Robust configuration-drift detection was likewise
absent. Discovering those gaps is a large part of what convinced us a rewrite was
warranted.

## Decision

The write strategy differs by CRD type, reflecting their different latency and
throughput requirements ([[0017-heartbeat-data-crd-split]]):

**Heartbeat CRDs** use a hand-rolled optimistic write: try a PUT with the
resourceVersion remembered from the last successful write; on cache-miss, 409, or
404 fall back to a GET then PUT/create. The cache is a `map[uuid]string` of UIDs
(used for owner references) maintained directly by the exporter, invalidated on
delete and on conflict. No controller-runtime involved — we control exactly when
it talks to the network.

**Domain CRDs** use `controllerutil.CreateOrPatch` via the controller-runtime
client, managed by `DomainWriteController`. The controller-runtime informer cache
handles resourceVersion tracking; we do not maintain a separate RV cache for this
path. This is the "adopt a runtime for watch-based handling" path that this record
previously left open.

In both cases: do **not** mirror "what k8s knows" in a separate state map beyond
what is strictly needed — the UID cache for heartbeats is the minimum to set owner
references and detect domain-CRD existence.

## Consequences

There is no local mirror of Kubernetes state to fall out of sync
([[0012-observer-tracker-exporter-pipeline]] collapsed the old `*K8S` maps for
exactly this reason). On the heartbeat path, the cached UID removes a GET per
update in the steady state — the write we do most often becomes a single
round-trip — while the GET-fallback on cache-miss/conflict keeps correctness. On
the domain-CRD path, controller-runtime's informer cache provides the same
benefit without a hand-rolled cache, which is acceptable because the feedback load
from our own writes echoing back on the watch subscription is low for slow-changing
data CRDs. The prediction that "drift detection and retention likely require
watching the data CRDs" is now resolved: they do, and the controller-runtime
manager handles it ([[0029-drift-detection]]). Heartbeats deliberately stay off
that path ([[0020-fifo-congestion-queue]], [[0017-heartbeat-data-crd-split]]).

## Alternatives rejected

- **Port the old single-retry drift-check procedure.** Its give-up-and-retry-next-poll
  behaviour left XML stale after migration (an ownership-handover detection gap,
  not a resourceVersion one) and did not detect drift; re-implementing it as-is
  was a non-goal of the rewrite.
- **Never cache resourceVersion; GET before every PUT.** Simplest and always
  correct, but a permanent extra round-trip on the hottest write path (the
  heartbeat). Acceptable as a baseline; the UID cache is the decided target for
  steady-state efficiency on the heartbeat path.
- **Controller-runtime informers for the heartbeat CRD.** Adopted for domain CRDs
  (see above) but rejected for heartbeats: the heartbeat path is throughput-critical
  and we want explicit control over when it touches the network. Bridging
  controller-runtime's logger to slog ([[0027-custom-logging]]) mitigates the
  observability concern for the paths where we do adopt it.
