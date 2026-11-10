<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Fake libvirt

Executes [ADR 0023](../decisions/0023-fake-libvirt-real-xdr.md) (fake speaks real
XDR over a Unix socket). Grounded in `dev/fake-libvirt/` (verified 2026-08-14
against `fakelibvirt.go`, `internal/state/state.go`, `internal/rpc/`,
`cmd/fake-libvirtd/`).

The fake libvirt exists in two forms sharing the same `state.FakeState`:

- an **embeddable library** (`fakelibvirt.Server`) for in-process tests, and
- a **standalone daemon** (`cmd/fake-libvirtd`) for `virsh`-style manual checks.

Both speak real XDR on a Unix socket, so the operator (and any `go-libvirt`
client) connects unmodified.

## Library API

```go
srv := fakelibvirt.NewServer(st)   // st *state.FakeState; nil ⇒ 3 default test domains
err := srv.Listen(socketPath)      // net.Listen("unix", …), one goroutine per conn
st  := srv.State()                 // *state.FakeState
err  = srv.Close()                 // closes listener + all conns, waits for goroutines
```

- `NewServer(nil)` loads three default domains: `test-vm-running`,
  `test-vm-shutoff`, `test-vm-paused`.
- `NewEmpty()` / `NewStateWithDomains([]*state.Domain)` build alternative states.

## Runtime state manipulation

Mutating `FakeState` publishes the lifecycle event a real libvirt would, so tests
drive the operator's event path:

| Call | Effect | Event published |
|------|--------|-----------------|
| `AddDomain(*Domain) error` | domain appears in list | `Defined` (0) |
| `RemoveDomain([16]byte) error` | domain leaves list | `Undefined` (1) |
| `SetDomainState([16]byte, uint8) error` | new state on GetInfo/GetState | mapped event (see below) |
| `PublishEvent(LifecycleEvent)` | direct injection | as given |
| `HideFromList` / `ShowInList([16]byte)` | visible to Get but not List | none |

`AddDomain` rejects duplicate UUID **and** duplicate name (returns an error).
`SetDomainState` publishes an event **only for mapped transitions** — e.g.
→Running = `Started` (2), Running→Shutoff = `Stopped` (5), Paused/Resumed,
`Shutdown` (6), `Crashed` (8). Unmapped transitions (e.g. Shutoff→Paused) change
the value silently with no event.

> Signature note: `RemoveDomain`/`SetDomainState`/`GetDomain` key on `[16]byte`
> UUIDs, not strings (the former openspec proposal wrote `RemoveDomain(uuid)`
> loosely).

## Events

`ConnectDomainEventCallbackRegisterAny` registers a callback per connection; a
per-connection `eventPusher` goroutine writes `DomainEventCallbackLifecycle`
(proc 318) messages to that client. Delivery requires a registered callback;
events are dropped if the per-connection buffer (100) fills. Multiple concurrent
clients each list independently and receive their own events.

## RPC procedures handled (library `dispatch`)

Beyond the connect handshake (`AuthList`, `ConnectOpen/Close`, `GetVersion`,
`GetLibVersion`, `GetUri`, `SupportsFeature`): `ConnectListAllDomains`,
`DomainGetInfo`, `DomainGetState`, `DomainLookupByName`, `DomainGetXMLDesc`,
`DomainGetOsType`, `DomainGetAutostart`, `DomainIsPersistent`,
`DomainHasManagedSave`, `NodeGetSecurityModel`, event register/deregister,
`ConnectRegisterCloseCallback` (acked, no-op). Unhandled procedures return an
XDR error reply.

> **Library vs daemon divergences to know:** the library's `SupportsFeature`
> returns `1` (supports all); the daemon returns `0`. The library's
> `DomainGetXMLDesc` returns a generic hardcoded XML (zero UUID); the daemon
> builds real per-domain XML. Prefer the daemon for XML-fidelity checks.

## Standalone daemon

`cmd/fake-libvirtd` serves the same protocol for `virsh -c unix:./fake.sock …`,
loads state from `--state-dir` (see [lifecycle-traces](lifecycle-traces.md) /
golden state), and exposes `ForceEvent(...)` for manual event injection. There is
also an optional REST/SSE API (`dev/fake-libvirt/api/`) used by the demo UI
([ADR 0028](../decisions/0028-tracing-and-timeline-ui.md)).

## `Domain` fields

`UUID [16]byte`, `Name string`, `ID int32`, `State uint8`, `MaxMem uint64`,
`Memory uint64`, `NrVirtCPU uint16`, `CPUTime uint64`.
