<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0036 — Fan-out in operator.Run: heartbeat channel vs controller-runtime event channel

Status: accepted · Date: 2026-09

## Context

The tracker emits all updates — both domain state changes and heartbeats — onto a
single `updateCh`. With the move to `DomainWriteController` under
controller-runtime ([[0034-controller-runtime-selective-adoption]]), domain
updates must reach the controller as `TypedGenericEvent[*types.Update]` on a
channel that controller-runtime watches as a source. Heartbeat updates must reach
`Exporter.RunHeartbeats` as plain `types.Update` values on a separate channel.
The two consumers have incompatible channel types and incompatible processing
models — the controller is event-driven and managed by the framework; the
heartbeat loop is a simple hand-rolled goroutine.

Splitting at the source (the tracker) was considered but rejected: the tracker
should not know about downstream consumers or their channel types. The natural
place is `operator.Run`, which already wires all components together.

## Decision

`operator.Run` starts a dedicated fan-out goroutine that reads from `updateCh`
and routes by update type: `UpdateHeartbeat` goes to `heartbeatCh`; all other
update types go to `updateEventCh` for the `DomainWriteController`.

```
┌──────────┐  obsCh   ┌──────────┐  updateCh  ┌─────────────┐
│ Observer │─────────▶│ Tracker  │───────────▶│   fan-out   │
└──────────┘          └──────────┘            └──────┬──────┘
                                                     │
                              heartbeatCh            │  updateEventCh
                         ┌───────────────────────┐   │  (TypedGenericEvent)
                         │  Exporter.RunHeartbeat│◀──┤
                         │  (hand-rolled loop)   │   │
                         └───────────────────────┘   │
                                                     │
                        ┌─────────────────────────┐  │
                        │  ctrl.Manager           │◀─┘
                        │  ├ DomainWriteController│
                        │  ├ DriftDetection       │
                        │  └ DeleteRetention      │
                        └─────────────────────────┘
```

Both downstream channels are buffered to the same capacity as `updateCh` to
avoid the fan-out goroutine becoming a bottleneck.

## Consequences

The tracker and its channel remain type-agnostic; the routing logic is visible in
one place in `operator.Run`. Adding a third consumer in future is a single case
in the fan-out switch. The cost is a goroutine boundary between the tracker and
its consumers, but this already existed — the tracker has always written to a
buffered channel consumed asynchronously downstream.

## Alternatives rejected

- **Split at the tracker.** Would require the tracker to import consumer-specific
  types (including a controller-runtime event type), inverting the dependency
  direction and coupling the core pipeline to a framework choice.
- **Single channel with interface values; consumers filter.** Each consumer would
  need to ignore messages not meant for it, and the channel type would need to
  accommodate both payloads — more complexity than a dedicated fan-out goroutine.
