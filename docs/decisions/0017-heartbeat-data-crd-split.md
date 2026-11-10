<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0017 — Split into a fast "heartbeat" CRD and slow "data" CRD(s)

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the organizational driver below is
recollection with no written trace.

## Context

We want frequently-refreshed proof that a VM's observed state is current — a
timestamped heartbeat that, when seen in Kubernetes, guarantees the exporter
actually made that observation ([[0016-time-slice-guarantee]]). That pulls toward
writing often. At the same time the full libvirt XML (and future derived data:
volumes, ports, devices) is large and changes rarely, which pulls toward writing
seldom. A single CRD carrying both would either update the whole large object on
every heartbeat or under-report freshness.

There is also an organizational force, stated plainly because it genuinely shaped
the design: "performance" is a frequent but *unfalsifiable*
objection to any Kubernetes/CRD proposal. The API server's
actual CRD update-rate limits are rarely benchmarked, and it is not a top priority to
find out ([[0010-export-libvirt-state-to-crds]]). A design that lets us tune how
much data moves at what frequency — without first having to win that debate — is
worth more here than a theoretically optimal single object.

## Decision

Split the export into a small, fast-updating **heartbeat** CRD (state,
timestamps, source, XML hash — the freshness proof) and one or more larger,
slow-updating **data** CRDs (a representation of the XML and, later, derived
resources), updated only when their content hash changes. The heartbeat carries
the hash of the raw XML so consumers can cross-check that a data CRD they hold is
still current. Because the data CRD may expose a *representation* rather than the
raw XML string, that same raw-XML hash is also stored in the data CRD — the
cross-check compares stored hashes, it cannot recompute one from the
representation ([[0033-structured-xml-representation]]).

## Consequences

Fields can be moved between "fast/cheap" and "slow/large" as an axis we control,
so freshness and API load are tunable without a rebuild and without settling the
performance argument up front. Consumers typically watch the data CRDs and, for
debugging, additionally watch the heartbeat. The costs: two-plus objects per VM
(more API objects, more cleanup — mitigated by owner references), and a
consumer-side verification pattern (compare data hash against the heartbeat's
`value`/`prevValue`) that we must keep honest. Retention and drift-detection may
later push the data CRDs toward a controller-style watch
([[0020-fifo-congestion-queue]], [[0021-optimistic-k8s-writes]]).

## Alternatives rejected

- **One CRD per VM carrying everything.** Either bloats every heartbeat with the
  full XML or hides staleness; also the thing we reversed for other reasons
  ([[0018-host-uuid-crd-naming]]).
- **Win the performance debate first, then design.** The limits are unmeasured
  and the debate is unfalsifiable; the split sidesteps the stall instead of
  waiting it out.
