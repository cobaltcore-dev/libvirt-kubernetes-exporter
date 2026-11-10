<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0018 — CRD naming `$host-$uuid` (single-CRD-per-VM reversed)

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; this records a reversal of an earlier
decision.

## Context

The natural first choice was one CRD per VM, keyed on the VM's UUID — one object,
one virtual machine. In a healthy system that holds: a UUID is defined on exactly
one host at a time. But this operator's job is to report reality faithfully
([[0011-scope-faithful-export-only]]), and reality violates that assumption: during
resize and live-migration a domain is briefly *defined on two hosts at once*, and
failed operations leave *orphaned definitions* behind. Worse, the two definitions
can carry *different XML*, which is precisely the divergence we most want to
capture, not collapse. Our QA/dev environment makes these overlaps routine;
production is cleaner but not immune.

A single UUID-keyed CRD forces those simultaneous, differing observations to fight
over one object — last writer wins, and the divergence we care about is lost.

## Decision

Name the resources `$host-$uuid` — one CRD per (host, VM) pair rather than per VM.
Each host's view of a domain is its own object, so two hosts observing the same
UUID during a migration produce two coexisting CRDs with their own XML and
timestamps.

## Consequences

Multi-host definitions and their XML differences become directly observable — the
faithful behaviour we want — instead of being flattened away. This reinforces the
scope boundary: reconciling the two views into "the one true VM" is explicitly the
consuming layer's job ([[0011-scope-faithful-export-only]]), and consumers must
expect multiple CRDs per UUID and select/group by the `uuid` label. The cost is
more objects and the need for host-scoped listing and cleanup (orphan detection
filters by the host label so one operator never reaps another host's CRDs,
[[0019-extended-states-and-retention]]). The `$host-$uuid` scheme is the base name
that the runtime suffixing for per-deployment isolation then builds on
([[0004-crd-suffix-runtime-naming]]).

## Alternatives rejected

- **One CRD per VM, keyed on UUID.** The original choice; reversed because it
  cannot represent a UUID legitimately present on two hosts with differing XML,
  and silently loses the divergence during migration/resize.
