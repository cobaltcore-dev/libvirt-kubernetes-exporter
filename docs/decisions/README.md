<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Architecture Decision Records

Concise, prose-style ADRs (Nygard form): each records *why* a load-bearing
decision was made and *what alternatives were rejected* — the knowledge the code
itself cannot hold.

**Provenance — two families, two strengths.** These ADRs come from two passes
with *different* reliability, and each record states its own provenance in its
header:

- **0001–0009 (packaging / CRD-naming / dev-deployment)** were captured from the
  live design conversation of that work (2026-08), while the reasoning was still
  available — rationale is *evidenced, not inferred*.
- **0010–0028 (core architecture, testing, tooling)** were reconstructed
  *post-hoc on 2026-08-14* from repository artifacts + git history and the
  author's recollection. The *decisions* are
  evidenced by code and docs, but much of the *why* is inferred or recalled, and
  some records are explicitly **not accepted** — treat their rationale as weaker
  than 0001–0009. Statuses vary: most `accepted`, but 0025 is `deferred`, 0028 is
  `partial / aspirational`, 0031 is
  `partially active`, and 0032 and 0033 are `open / proposed`. Several accepted
  records (0030, 0031) are implemented but **not yet fully wired into
  `operator.Run`** — each says so in its status line.

## Index — core architecture, testing & tooling (0010–0033)

Ordered by logical priority: the "why this exists" first, then the mechanisms
that follow from it.

| ADR | Title | Status | Depends on |
|-----|-------|--------|-----------|
| [0010](0010-export-libvirt-state-to-crds.md) | Export libvirt VM state to Kubernetes CRDs | accepted | — |
| [0011](0011-scope-faithful-export-only.md) | Scope boundary: faithful export only; reconciliation deferred to consumer | accepted | 0010 |
| [0012](0012-observer-tracker-exporter-pipeline.md) | Observer → Tracker → Exporter pipeline in separate packages | accepted | 0010 |
| [0013](0013-single-threaded-state-ownership.md) | Single-threaded state ownership with channel handoff | accepted | 0012 |
| [0014](0014-dual-source-observation.md) | Dual-source observation: poll for existence, events for latency | accepted | 0010, 0019 |
| [0015](0015-observedvalue-one-level-history.md) | `ObservedValue[T]`: one level of history per field | accepted | 0016 |
| [0016](0016-time-slice-guarantee.md) | Time-slice guarantee: `q`/`t` samples, `t0→q1` windows | accepted | 0010, 0015 |
| [0017](0017-heartbeat-data-crd-split.md) | Split into a fast heartbeat CRD and slow data CRD(s) | accepted | 0010, 0016 |
| [0018](0018-host-uuid-crd-naming.md) | CRD naming `$host-$uuid` (single-CRD-per-VM reversed) | accepted | 0011 |
| [0019](0019-extended-states-and-retention.md) | Extended states and retention-before-delete | accepted | 0010, 0016 |
| [0020](0020-fifo-congestion-queue.md) | FIFO map-coalescing congestion queue (heap dropped) | accepted | 0012 |
| [0021](0021-optimistic-k8s-writes.md) | Optimistic K8s writes with cached resourceVersion | accepted | 0012, 0017 |
| [0022](0022-fake-libvirt-and-fake-k8s.md) | Fake libvirt + fake Kubernetes instead of a real cluster | accepted | — |
| [0023](0023-fake-libvirt-real-xdr.md) | Fake libvirt speaks real XDR over a Unix socket | accepted | 0022 |
| [0024](0024-scenario-based-testing.md) | Scenario-based testing of distributed-systems glitches | accepted | 0022 |
| [0025](0025-deterministic-stepping-deferred.md) | Deterministic stepping / full lockstep + fuzzing | deferred | 0024 |
| [0026](0026-separate-dev-tooling.md) | Separate `dev/` tooling from the production operator | accepted | — |
| [0027](0027-custom-logging.md) | Roll our own `slog`-based logging vs controller-runtime | accepted | — |
| [0028](0028-tracing-and-timeline-ui.md) | Span tracing + timeline web UI for knowledge transfer | aspirational | 0024 |
| [0029](0029-drift-detection.md) | Drift detection via controller-runtime reconciler | accepted | 0021 |
| [0030](0030-conflict-detection-ownership.md) | Conflict detection + ownership annotation, immediate takeover | accepted (tests absent) | 0018, 0021 |
| [0031](0031-load-spreading.md) | Load spreading: stagger / throttle / rate-limit over coalescing | partially active | 0017, 0020 |
| [0032](0032-uuid-type-labels.md) | Stamp related-resource UUIDs as `$uuid: $type` labels | open / proposed | 0017 |
| [0033](0033-structured-xml-representation.md) | Structured XML representation in the data CRD (+ Nova metadata) | open / proposed | 0017 |
| [0034](0034-controller-runtime-selective-adoption.md) | Selective adoption of controller-runtime for domain writes, drift, and retention | accepted | 0021, 0027, 0029 |
| [0035](0035-domain-write-pending-map-coalescing.md) | Domain write coalescing via pending map, not the CongestionQueue | accepted | 0020, 0034 |
| [0036](0036-operator-fanout.md) | Fan-out in operator.Run: heartbeat channel vs controller-runtime event channel | accepted | 0034 |
| [0037](0037-envtest-and-fake-k8s-boundaries.md) | Restore fault injection after controller-runtime adoption | open / temporary | 0022, 0024, 0034, 0036 |
| [0038](0038-config-validation-and-normalization-boundaries.md) | Define boundaries for config validation and normalization | open | 0034, 0036 |
| [0039](0039-nova-metadata-labels-and-status.md) | Project Nova metadata as labels and data status | accepted | 0017, 0032, 0033 |
| [0040](0040-migration-and-state-reason-status.md) | Migration and state-reason status via additional API calls | accepted | 0011, 0014, 0017 |

## Index — packaging, CRD naming & dev deployments (0001–0009)

The table is ordered roughly by **logical priority** (the driving decision
first, then what follows from it), *not* by ADR number. Numbers are fixed and
reflect the order the records were written — the two orderings differ because
the root "why" (0009) was recognised and written after the mechanisms it
explains.

| ADR | Title | Status | Depends on |
|-----|-------|--------|-----------|
| [0009](0009-configurable-dev-deployments.md) | Configurable per-branch dev deployments on the shared QA cluster | accepted | — |
| [0001](0001-crdmeta-single-source-of-truth.md) | `crdmeta` as the single source of truth for CRD naming | accepted | — |
| [0002](0002-controller-gen-cli-only.md) | Generate CRDs with the controller-gen CLI, not controller-runtime | accepted | 0001 |
| [0003](0003-hard-rename-crds-libvirtdomains.md) | Hard-rename base CRDs to `libvirtdomains` | accepted | 0001, 0002 |
| [0004](0004-crd-suffix-runtime-naming.md) | Per-deployment CRD isolation via runtime `CRD_SUFFIX` + pipeline sed | accepted | 0001, 0003, 0009 |
| [0005](0005-cluster-scoped-resource-isolation.md) | Isolate cluster-scoped resources by release namespace | accepted | 0004, 0009 |
| [0008](0008-namespace-agnostic-chart.md) | Keep the chart namespace-agnostic (operator target = release namespace) | accepted | — |

## Decision graph

### Core architecture, testing & tooling

```
0010 export as CRDs ──┬─▶ 0011 scope boundary ──▶ 0018 $host-$uuid naming ──▶ 0004 CRD_SUFFIX
                      ├─▶ 0012 pipeline ──┬─▶ 0013 single-writer ──▶ 0025 stepping (deferred)
                      │                   ├─▶ 0020 FIFO queue
                      │                   └─▶ 0021 optimistic writes
                      ├─▶ 0016 time-slice ──┬─▶ 0015 ObservedValue
                      │                     └─▶ 0017 heartbeat/data split ──▶ 0021
                      ├─▶ 0019 extended states + retention ──▶ 0014 dual-source
                      └─▶ 0017 heartbeat/data split

0022 fakes over real cluster ──┬─▶ 0023 real XDR (golden traces)
                               └─▶ 0024 scenario testing ──┬─▶ 0025 stepping (deferred)
                                                           └─▶ 0028 tracing/timeline UI (aspirational)
0013 single-writer ───────────────────────────────────────▶ 0024 (correctness claim)
0026 separate dev/ tooling (independent)
0027 custom logging (open; relates to 0021 controller-runtime distrust)

0021 optimistic writes ──▶ 0029 drift detection (periodic reconcile + RV compare)
0018 $host-$uuid ────────▶ 0030 conflict detection (ownership annotation, takeover)
0021 optimistic writes ──▶ 0030
0017 heartbeat/data split ─┬─▶ 0031 load spreading (stagger / throttle / rate-limit)
0020 FIFO queue ───────────┘   (coalescing is the always-on base of 0031)
0017 heartbeat/data split ──▶ 0032 $uuid:$type labels (proposed; cardinality open question)
0017 heartbeat/data split ──▶ 0033 structured XML representation (proposed; hash stored, not derived)
0022 fake-k8s ─┬─▶ 0037 envtest/fake-k8s boundaries
0024 scenarios ┘
0034 controller-runtime ─┬─▶ 0036 fan-out
                         └─▶ 0037
0036 fan-out ───────────────▶ 0037
0034 controller-runtime ─┬─▶ 0038 config normalization boundaries
0036 fan-out ────────────┘
0017 heartbeat/data split ─┬─▶ 0039 Nova metadata labels/status
0032 UUID labels (open) ───┤
0033 structured XML/Nova ──┘
```

Runtime-status caveat: 0030 (conflict) and 0031 (stages 2–4) are
implemented but **only partially wired into `operator.Run`** — see each record's
status line. They are decided and built, not fully activated.

The core-architecture cluster bridges into the packaging cluster at
**0018 → 0004**: the `$host-$uuid` base name is what the runtime `CRD_SUFFIX`
mechanism then suffixes for per-deployment isolation.

### Packaging, CRD naming & dev deployments

```
0009 dev deployments ─┬─▶ 0004 CRD_SUFFIX ──▶ 0005 cluster-scoped isolation
                      └─(motivates the isolation surfaces below)

0001 crdmeta ──┬─▶ 0002 controller-gen ──┐
               │                         ├─▶ 0003 hard-rename ──▶ 0004 CRD_SUFFIX
               └─────────────────────────┘

0008 namespace-agnostic (independent)
```
