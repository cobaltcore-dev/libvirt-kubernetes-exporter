<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0014 — Dual-source observation: poll for existence, events for latency

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; the CH-driver motivation is evidenced
by `requirements/adr.md` and recollection.

## Context

Libvirt offers two ways to learn a domain's state: lifecycle *events* (pushed,
low-latency) and *polling* (`ConnectListAllDomains` + `DomainGetInfo`, complete
but slow). Neither suffices alone. Events are the lower-latency signal but are
*leaky* — the cloud-hypervisor driver we run does not emit every event, events
are lost across reconnects and live-migration, and libvirt itself races. Polling
is complete but has high latency and load, and on its own would miss short-lived
transitions. This "listen for speed, poll to be sure" pairing is a pattern we
reach for in several places.

The subtlety is what each source is *authoritative* for. If we simply let the
most recent observation win, a poll that started before an event but finished
after it will overwrite fresher event data with stale state.

## Decision

Run both a poller and an event listener, feeding the same observation channel.
Split authority by concern: **polling is authoritative for existence** — if a
poll does not see a VM, it is gone, regardless of events — while **events take
short-term precedence for the state of existing VMs**. Concretely, a poll-sourced
*state* update is muted if a recent event already set the state within a discard
threshold (per-UUID, compared on the observation's `observed` time so a poll that
began before the event is correctly discarded). Existence/deletion signals from
polling are *never* muted.

## Consequences

Missed events are corrected by the next poll (completeness), while events keep
latency low (responsiveness), and the poll-overwrites-event race is closed without
a global lock. The care point, learned in implementation, is that muting must
apply to *state* only, never to existence: an `IsDeleted` poll observation must
bypass muting or a just-migrated-away VM would linger. Muting is keyed per UUID
and per *state-changing* event, so an event for VM-A never mutes a poll for VM-B,
and an async XML update does not reset the mute window. A known gap: muting is
currently **one-directional** — an event mutes a *later* poll, but an event
arriving just *after* a poll does not retroactively suppress that poll. Handling
the truly-simultaneous case would require briefly *holding* poll observations so a
near-coincident event can fire first; this is rare and is not yet designed or
built. This interlocks with extended states and retention
([[0019-extended-states-and-retention]]) and with per-source scoping once multiple
drivers (CH, QEMU) are observed, since a QEMU poll must not mark a CH VM missing.

## Alternatives rejected

- **Events only.** Lowest latency, but the CH driver's leakiness means we would
  silently miss state changes and deletions — the core reliability failure we
  exist to avoid.
- **Polling only.** Complete but high-latency and higher load; misses transient
  transitions between polls.
- **Last-writer-wins with no muting.** Simplest, but reintroduces the
  poll-overwrites-fresh-event glitch under normal timing.
