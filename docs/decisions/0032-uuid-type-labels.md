<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0032 — Stamp related-resource UUIDs as `$uuid: $type` labels (proposed)

Status: open / under discussion · Date: 2026-08 · Provenance: a colleague's
proposal captured on 2026-08-15 for decision; **not implemented and not decided**.
The rationale below is the proposal as understood, not a settled conclusion.

## Context

A VM references many other OpenStack/libvirt entities by UUID — volumes, ports,
the root image, and the owning user/project. A recurring operational question is
the reverse lookup: *which VMs reference this volume/port/image?* Today that means
fetching each domain's data and scanning its XML/metadata, which `kubectl` and
watchers can't do natively.

Kubernetes makes label *selectors* the native way to answer "which objects match
this key/value?". So the proposal is to turn each referenced entity into a
queryable label. The twist is the encoding: rather than a fixed key with a value
(`volume: <uuid>`, which can only hold one volume), use the **UUID as the label
key and the entity type as the value** — `"<uuid>": "volume"`,
`"<uuid>": "port"`, etc. — so a single object can carry arbitrarily many
references and each is directly selectable (`kubectl get … -l <uuid>`). This
appeared in the original DESIGN.md heartbeat sketch (the `$uuid: $type` lines) and
is now raised again by a colleague.

## Decision

**Undecided.** The proposal is to stamp related-resource UUIDs as label keys with
their type as the value on the CRDs, enabling reverse lookup by selector. Whether
to adopt it hinges on the open question below.

## Consequences

If it works, reverse lookups ("all VMs on this volume") become one label selector,
no XML scanning — a genuinely useful capability for operators and debugging, and a
natural fit for the export's purpose ([[0010-export-libvirt-state-to-crds]]).

But the open risk is real and is the reason this is not yet accepted: using UUIDs
as *label keys* means the set of distinct label keys across the cluster is
**unbounded** — it grows with every volume, port, and image, not with a fixed
schema. Kubernetes/etcd indexes labels, and label keys also face syntactic limits
(a bare UUID is a valid key, but the design leans on many thousands of them). The
worry is API-server / etcd load and index bloat at our scale — precisely the
*unmeasured CRD-performance* question that already shapes the fast/slow split
([[0017-heartbeat-data-crd-split]]) and that tends to stall as an unfalsifiable
argument here. This decision should be settled by *measuring* (label cardinality
vs. API-server behaviour) rather than by assertion, and the harness that lets us
generate many VMs cheaply ([[0022-fake-libvirt-and-fake-k8s]]) is the place to do
it. Where the stamping would live (data CRD vs heartbeat) is also open; if it goes
on the heartbeat it interacts with the write-frequency concerns of
[[0017-heartbeat-data-crd-split]].

## Alternatives rejected / to weigh

- **Fixed-key labels (`volume: <uuid>`).** Bounded, well-understood key set, but a
  label value holds one string — cannot represent a VM's *many* volumes/ports;
  loses the multi-reference reverse lookup that is the whole point.
- **Annotations instead of labels.** Annotations can hold arbitrary many
  references without polluting the label index, but are **not selectable** — so
  they don't answer the reverse-lookup query, which defeats the purpose.
- **A separate index/CRD (reverse map: entity → VMs).** Keeps VM CRDs clean and
  bounds label cardinality, but adds a component to maintain and re-derives what
  labels would give for free; worth weighing if measurement rules labels out.
- **Do nothing (scan XML on demand).** The status quo; no API-server risk, but no
  native reverse lookup — the gap the proposal targets.
