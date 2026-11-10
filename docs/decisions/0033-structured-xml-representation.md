<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0033 — Structured XML representation in the data CRD (+ self-parsed Nova metadata)

Status: proposed / open (design direction; concrete representation undecided;
Nova projection partly settled by [[0039-nova-metadata-labels-and-status]]) · Date:
2026-08 · Provenance: reconstructed post-hoc on 2026-08-15 from recollection +
the prior implementation (`libvirt-kubernetes-exporter/cmd/lv-k8s-exporter/xml.go`:
`domainToMap`, `parseNovaMetadata`). The full XML representation is deliberately
not fixed here.

## Context

The data CRD carries the domain's configuration ([[0017-heartbeat-data-crd-split]]).
The obvious option is to store the raw XML libvirt returns as one string field —
but a string is opaque to Kubernetes: no field selectors, no readable diffs, no
way for a consumer to reach into a device or a disk without re-parsing XML
themselves. Since the goal is a *queryable* export ([[0010-export-libvirt-state-to-crds]]),
an opaque blob undercuts the point.

The `go-libvirt` / `libvirtxml` library already parses the domain into a Go
struct, so serializing *that* into Kubernetes' native untyped-JSON form is cheap —
the library did the heavy lifting. Two wrinkles shape the decision. First,
OpenStack/Nova metadata is **not** parsed by the libvirt library; it arrives as an
opaque `<metadata>` XML blob we must parse ourselves. Second, the freshness hash
([[0016-time-slice-guarantee]]) is computed over the **raw XML string** libvirt
returned — and if the data CRD exposes a *representation* rather than that raw
string, the hash cannot be recomputed from the CRD's contents.

## Decision

Represent the domain configuration in the data CRD as **structured (untyped)
JSON**, not a raw string — serialized from the parsed libvirt domain struct
(the prior implementation marshalled `libvirtxml.Domain` → JSON → map, stripped
nulls). Parse Nova metadata **ourselves** and fold it into that structure (the old
code lifted the `<nova:instance>` fields into `Metadata.Nova` and dropped the raw
metadata blob). The **concrete field layout is intentionally left open** here —
this ADR records the *direction* (structured, library-serialized, Nova parsed by
us), not the schema.

Because the exposed representation is not the raw string, **store the raw-XML hash
inside the data CRD too**, next to the representation. A consumer verifies
currency by comparing the heartbeat CRD's `xmlHash` against the data CRD's *stored*
hash — never by recomputing a hash from the representation, which would not match.

## Consequences

The config becomes queryable and diffable with ordinary Kubernetes tooling, at low
cost since the library does the parsing. We own the Nova parsing — a maintenance
cost and a drift risk if Nova's metadata format changes, accepted because no
library does it for us and the fields (project, user, flavor, ports, root image)
are what operators actually query by. The raw XML string is not necessarily
recoverable from the CRD; if a use-case needs the exact bytes, that must be an
explicit, separate field. The stored-hash requirement is a real coupling on
[[0017-heartbeat-data-crd-split]]: the data CRD must carry the hash-of-raw-XML
alongside whatever representation we choose, or the heartbeat/data cross-check
breaks.

## Alternatives rejected

- **Raw XML string in the data CRD.** Opaque to Kubernetes — no selectors, no
  readable diffs, forces every consumer to re-parse XML; defeats the queryable
  export goal.
- **A fully-typed CRD schema for every libvirt field.** Huge surface and brittle
  against libvirt version drift; untyped JSON absorbs schema evolution without a
  CRD change.
- **Recompute the hash from the stored representation.** Impossible once the raw
  string isn't exposed (serialization is lossy/normalising); hence the hash is
  stored, not derived.
