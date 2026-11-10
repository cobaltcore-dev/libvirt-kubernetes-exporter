<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# CRD schema reference

Reference for the custom resources the operator writes. This is the *what* (the
shape); for the *why* see the decision records: the CRD split
([ADR 0017](../decisions/0017-heartbeat-data-crd-split.md)), the naming
([ADR 0018](../decisions/0018-host-uuid-crd-naming.md)), the timestamp fields
([ADR 0016](../decisions/0016-time-slice-guarantee.md)), extended states
([ADR 0019](../decisions/0019-extended-states-and-retention.md)), the XML
representation ([ADR 0033](../decisions/0033-structured-xml-representation.md)),
and the naming source-of-truth
([ADR 0001](../decisions/0001-crdmeta-single-source-of-truth.md)).

**Design intent, with code divergences flagged.** This page describes the
*intended* shape; verified 2026-09 against `api/v1alpha1/crd.go`,
`pkg/types/domain.go`, and `pkg/crdmeta/meta.go`. The generated manifests are
under `charts/libvirt-kubernetes-exporter/crds/`. Where the current code diverges
from the design it is called out inline; those divergences are gaps to fix, not
the intended shape.

> **Both CRDs are `status`-only — no `spec`.** Everything the operator writes is an
> *observation/export*, so nothing is user-authored `spec`.

> **Note on the `2` suffix and old docs.** All kinds/plurals carry a `2`
> (`LibvirtDomain`, `libvirtdomains`, …) to avoid clashing with the previous
> implementation's CRDs during the hard rename
> ([ADR 0003](../decisions/0003-hard-rename-crds-libvirtdomains.md)). The former
> `requirements/` docs described a host-level heartbeat CRD that does not exist;
> only a per-VM heartbeat exists.

## Group / version / naming

- **Group** `kvm.cloud.sap`, **version** `v1alpha1` (`pkg/crdmeta/meta.go`).
- Base plurals: `libvirtdomains`, `libvirtdomainheartbeats`. At runtime a
  `CRD_SUFFIX` env var is appended to plurals (and PascalCased onto kinds) for
  per-deployment isolation ([ADR 0004](../decisions/0004-crd-suffix-runtime-naming.md)).
- **Resource name:** `$host-$uuid` (e.g. `node001-2bd0d57d-…`). Both CRDs are
  `Namespaced`.
- **Labels** on both CRDs (prefix `libvirt.kvm.cloud.sap`):
  `libvirt.kvm.cloud.sap/host`, `…/uuid`, `…/name`, and selected Nova metadata
  labels `…/nova-name`, `…/nova-flavor`, `…/nova-project`, `…/nova-user` when the
  values are valid Kubernetes label values. There is **no** `driver` label (that
  was the old implementation).
- **Ownership annotation** (written by the exporter on the heartbeat CRD,
  [ADR 0030](../decisions/0030-conflict-detection-ownership.md)):
  `operator.kvm.cloud.sap/owner`. Current code stamps the hostname; the intended
  value is a random per-instance id (so two same-host operators are
  distinguishable).

## `LibvirtDomain` — data CRD (slow-updating)

`metadata` + `status` only (no `spec`); updates only when the definition changes.
Status subresource enabled. Printer columns: UUID, Host, State. `uuid`/`name`/`host`
live in metadata/labels, not in a spec.

**Status** is intended to carry:
- the derived state fields: `state` (string — the `LibvirtState.String()`),
  `stateCode` (uint8 — the numeric state), `maxMemoryKB`/`memoryKB` (uint64,
  omitempty), `vcpus` (uint16, omitempty), `lastChanged` (RFC3339Micro string,
  omitempty), and `novaMetadata` (self-parsed Nova metadata);
- **a representation of the domain XML** — structured JSON serialized from the
  libvirt library, plus self-parsed Nova metadata under `novaMetadata`, *not* a raw string
  ([ADR 0033](../decisions/0033-structured-xml-representation.md); representation
  undecided);
- **the hash of the raw XML** libvirt returned. This hash must be stored here
  because it is computed over the raw string (which the CRD may not expose) and so
  cannot be recomputed from the representation. Consumers cross-check currency by
  comparing the heartbeat's `xmlHash` against this stored hash.

> **Code divergence.** The current code stores the parsed XML representation under
> `status.domainXML` and stores the raw XML hash under `status.xmlHash`; the
> broader structured representation is still evolving
> ([roadmap](../ROADMAP.md), [ADR 0033](../decisions/0033-structured-xml-representation.md)).

## `LibvirtDomainHeartbeat` — heartbeat CRD (fast-updating)

**No `spec`** — `metadata` + `status` only. Updates frequently with observation
timestamps. Printer columns: Observed, TTL.

**Status** (`HeartbeatStatus`):
- `ttl` (string) — desired update interval.
- `observed` (RFC3339Micro string) — max of all sub-observations.
- `host` / `driver` (strings, omitempty) — small identity/debug fields.
- `status` (`ObservationRecord`) — the domain-state observation.
- `xmlHash` (`ObservationRecord`, omitempty) — the XML-hash observation.

Nova metadata is intentionally **not** duplicated in heartbeat status. It appears
on heartbeat metadata as labels for `kubectl` filtering and printer columns; the
canonical parsed metadata lives in `LibvirtDomain.status.novaMetadata`
([ADR 0039](../decisions/0039-nova-metadata-labels-and-status.md)).

### Example render (current schema)

A representative `LibvirtDomainHeartbeat` object as the operator writes it today:

```json
{
  "apiVersion": "kvm.cloud.sap/v1alpha1",
  "kind": "LibvirtDomainHeartbeat",
  "metadata": {
    "name": "node001-2bd0d57d-873e-4ac6-bf0a-acc847f74f27",
    "namespace": "libvirt-exporter",
    "uid": "uid-42",
    "labels": {
      "libvirt.kvm.cloud.sap/host": "node001",
      "libvirt.kvm.cloud.sap/uuid": "2bd0d57d-873e-4ac6-bf0a-acc847f74f27",
      "libvirt.kvm.cloud.sap/name": "instance-2bd0d57d",
      "libvirt.kvm.cloud.sap/nova-name": "example-instance",
      "libvirt.kvm.cloud.sap/nova-flavor": "m1.large",
      "libvirt.kvm.cloud.sap/nova-project": "example-project",
      "libvirt.kvm.cloud.sap/nova-user": "example-user"
    },
    "annotations": {
      "operator.kvm.cloud.sap/owner": "node001"
    }
  },
  "status": {
    "ttl": "10s",
    "observed": "2026-08-15T12:00:01.123456Z",
    "status": {
      "value": "running",
      "queried": "2026-08-15T12:00:01.000000Z",
      "observed": "2026-08-15T12:00:01.123456Z",
      "src": "poll",
      "prevValue": "paused",
      "prevQueried": "2026-08-15T11:59:51.000000Z",
      "prevObserved": "2026-08-15T11:59:51.100000Z"
    },
    "xmlHash": {
      "value": "a1b2c3d4",
      "queried": "2026-08-15T12:00:01.000000Z",
      "observed": "2026-08-15T12:00:01.123456Z",
      "src": "poll"
    }
  }
}
```

> **Not yet present (from the old DESIGN.md sketch).** The original heartbeat
> design imagined more: per-resource UUID labels (`"<uuid>": "volume"`), a
> `libvirtDriver` label, and future `status.job` (domjobinfo, for migrations) and
> `status.pid` observation records. **None of these exist in the current code** —
> the owner annotation is a hostname today (intended: a per-instance id,
> [ADR 0030](../decisions/0030-conflict-detection-ownership.md)); the
> `$uuid: $type` labels are a proposal under discussion
> ([ADR 0032](../decisions/0032-uuid-type-labels.md)).

### `ObservationRecord` — the CRD projection of `ObservedValue[T]`

The JSON shape carrying one value plus its timing and one level of history
([ADR 0015](../decisions/0015-observedvalue-one-level-history.md),
[ADR 0016](../decisions/0016-time-slice-guarantee.md)):

`value` (string), `queried` (string), `observed` (string), `src` (string:
`poll`/`event`/`startup`/`unknown`), and — only when a previous observation
exists — `prevValue`, `prevQueried`, `prevObserved` (all omitempty). Timestamps
are RFC3339Micro. The guaranteed constant-state window is `prevObserved → queried`
(`t0 → q1`).

## `LibvirtState` values (`pkg/types/domain.go`)

Standard libvirt: `0 nostate`, `1 running`, `2 blocked`, `3 paused`,
`4 shutdown`, `5 shutoff`, `6 crashed`, `7 pmsuspended`.

Extended (synthetic, `IsExtended()` = value ≥ 250,
[ADR 0019](../decisions/0019-extended-states-and-retention.md)):
`250 unknown` (socket failure), `251 hanging` (operation timeout — reserved for
connection pooling), `252 gone` (observed before, now missing). `255 deleted`
exists but is **deprecated** in favour of `gone`. Any other value stringifies as
`invalid`. The numeric value appears directly in the data CRD's `status.stateCode`.
