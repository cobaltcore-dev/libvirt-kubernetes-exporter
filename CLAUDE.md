<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# CLAUDE.md

## Project Overview

This repository contains a Kubernetes operator that exports libvirt VM state to Kubernetes custom resources. The operator monitors VMs via libvirt's API and maintains CRDs representing VM state and heartbeats.

## Key Documentation

Design *intent* lives under `docs/` (the code is authoritative for *behaviour*):

- **docs/decisions/** — Architecture Decision Records (ADRs 0001–0040): *why* each
  load-bearing decision was made and *what alternatives were rejected*. Start at
  `docs/decisions/README.md` (index + decision graph + provenance/status notes).
  0001–0009 are the packaging/CRD-naming/dev-deploy cluster (captured live);
  0010–0040 are core architecture + testing, reconstructed post-hoc.
- **docs/reference/crd-schema.md** — the CRD schema (`LibvirtDomain`,
  `LibvirtDomainHeartbeat`, `ObservedValue`, extended states, labels).
- **docs/testing/** — how the test infrastructure works (fake-libvirt, fake-k8s,
  integration harness, golden traces; stepping is deferred).

## Architecture

```
┌──────────┐   obsCh    ┌──────────┐  updateCh  ┌─────────────┐   updateEventCh  ┌──────────────────────┐
│ Observer │ ─────────▶ │ Tracker  │ ─────────▶ │  fan-out    │ ───────────────▶ │ DomainWriteController│
│          │            │          │            │             │                  │ (controller-runtime) │
│ Poller   │            │ domains  │            │             │   heartbeatCh    └──────────────────────┘
│ Events   │            │   map    │            │             │ ───────────────▶ ┌──────────────────────┐
└──────────┘            └──────────┘            └─────────────┘                  │ Exporter             │
                                                                                  │ (heartbeats only)    │
                                                                                  └──────────────────────┘

controller-runtime Manager also runs:
  ┌──────────────────────────┐   ┌──────────────────────────┐
  │ DriftDetectionController │   │ DeleteRetentionController │
  │ (re-syncs CRD vs tracker)│   │ (retention before delete) │
  └──────────────────────────┘   └──────────────────────────┘
```

- **Observer** (`pkg/libvirt`): Polls libvirt + listens for lifecycle events; puts `Observation`s onto `obsCh`
- **Tracker** (`pkg/state`): Single-threaded state management; consumes `obsCh`, emits `Update`s onto `updateCh`
- **Fan-out**: Routes `UpdateHeartbeat` updates to `heartbeatCh`; all other domain updates to `updateEventCh` for the controller
- **DomainWriteController** (`internal/controllers`): controller-runtime reconciler that writes `LibvirtDomain` CRDs; coalesces rapid updates via a pending map (stale-drop)
- **Exporter** (`pkg/exporter`): Writes `LibvirtDomainHeartbeat` CRDs only, via `RunHeartbeats`
- **DriftDetectionController** (`internal/controllers`): Watches `LibvirtDomain` CRDs and re-syncs when they drift from tracker state
- **DeleteRetentionController** (`internal/controllers`): Holds deleted domains for a configured retention window before physical removal

## Libvirt API Usage

The observer uses these libvirt procedures per observation cycle:
- `ConnectToURI` — connect to the libvirt socket (with URI override support)
- `ConnectListAllDomains` — poll for the full VM list
- `DomainGetInfo` — VM state, memory, vCPUs (called per domain)
- `DomainGetXMLDesc` — full VM configuration XML (called per domain)
- `DomainGetState` — state reason code, e.g. paused-by-watchdog vs paused-by-user (called per domain, unconditional)
- `DomainGetJobStats` — detect active migrations and their direction (called per domain, unconditional)
- `DomainLookupByUUID` — existence check for event-triggered observations
- `LifecycleEvents` — event stream for low-latency state change notifications

## Development Workflows

### Running the demo

```bash
cd integration/cmd/demo
go run . --webui-dir ../../../dev/webui
# Web UI at http://localhost:8000
# APIs: harness:8080, libvirt:8081, k8s:8082, operator:8083
```

### Running integration tests

```bash
cd integration
go test -v
```

## Testing Strategy

Integration tests use **fake-libvirt** (real XDR protocol) against the operator pipeline. The Kubernetes side is split:

- **envtest** (controller-runtime) — primary path for domain write, drift detection, delete retention, and throughput tests
- **fake-k8s** (legacy HTTP harness) — most tests here are skipped pending migration to envtest; preserved for future fault-injection work (socket hangs, K8s error injection)

## Conventions

- Use `go-libvirt` directly (not CGO bindings)
- Single-threaded state modification (Tracker.Run)
- Deep copies through channels
- Package separation: `types`, `libvirt`, `state`, `exporter`
- CRD naming: `$hostname-$uuid`
