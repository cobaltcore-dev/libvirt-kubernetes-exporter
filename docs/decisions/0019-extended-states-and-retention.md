<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0019 — Extended states and retention-before-delete

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; evidenced by `requirements/adr.md`,
`dev/feat-advanced-notes/{FRICTION,ERRATA1}.md`, and commits `ae1ddc3`,
`cfb7baa`.

## Context

Standard libvirt states (running, shutoff, …) describe a VM we can currently
observe. But faithfulness ([[0010-export-libvirt-state-to-crds]],
[[0016-time-slice-guarantee]]) means also representing the states where we
*cannot* observe cleanly: the socket is down, a poll hung, or a VM we used to see
is simply gone. Collapsing those into "shutoff" or silently dropping the record
would be a lie of exactly the kind this operator exists to prevent — a consumer
must be able to tell "libvirt says shutoff" from "we can't reach libvirt" from "it
vanished."

There is also a restart tension. The *eager-faithful* stance says a fresh
operator has observed nothing, so prior state is `unknown` until re-observed —
honest, but it blanks the board on every restart. The *practical* stance says
adopt the last-known state from Kubernetes and carry on, accepting an
observational gap. For debugging we want *both* truths available rather than
picking one.

## Decision

Add operational states beyond libvirt's own: `unknown` (socket/connection
failure), `hanging` (poll timeout), and `gone` (previously observed, now absent
from the list). On disappearance we do not delete immediately: mark the VM `gone`,
stamp `deletedAt`, keep the CRDs during a configurable retention window, and only
then emit the delete. A VM reappearing during retention clears `deletedAt` and
resumes normal state.

## Consequences

Failure and absence become first-class, queryable facts, and the export stops
lying by omission. Retention gives an observability grace period — a
recently-vanished VM (including a brief disappearance during live-migration)
stays inspectable instead of blinking out — and, per the restart tension, lets us
surface the faithful `unknown` while still being able to consult the practical
last-known value from Kubernetes. The costs are real overlaps to keep straight:
`IsDeleted` (input signal) vs `gone` (visible state) vs `deletedAt` (internal
timer) vs the eventual delete (output) are four representations of "gone" that
must stay coordinated ([[0014-dual-source-observation]] keeps existence signals
un-muted so this pipeline is fed correctly), and retention interacts with orphan
cleanup — a retained VM is still tracked, so it must not be reaped as an orphan.
`gone` also supersedes an earlier synthetic `deleted` sentinel, now effectively
deprecated.

## Alternatives rejected

- **Standard libvirt states only.** Cannot distinguish "unreachable" from
  "shutoff" from "vanished"; silent gaps violate the faithfulness promise.
- **Delete immediately on disappearance.** Loses all trace of recently-gone VMs
  (debugging, audit) and mishandles transient migration blips that reappear.
- **Pick one restart policy.** Choosing eager-only blanks state on restart;
  practical-only hides the observational gap. Exporting both keeps debugging
  honest.
