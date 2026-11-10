<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Testing infrastructure reference

Behavioural documentation for the test infrastructure — the fakes, the harness,
and the trace tooling. Unlike the [decision records](../decisions/) (which record
*why*), these pages describe *what the tooling does* so a test author can use it.

**Provenance.** These pages were reconstructed on 2026-08-14 by verifying the
former `openspec/changes/*` proposals against the actual code, then rewriting to
match reality (the proposals had drifted). Each page notes where it is grounded.
They *execute* the testing ADRs:

| Page | Executes ADR | Code |
|------|--------------|------|
| [fake-libvirt](fake-libvirt.md) | [0023](../decisions/0023-fake-libvirt-real-xdr.md) real XDR | `dev/fake-libvirt/` |
| [fake-k8s](fake-k8s.md) | [0022](../decisions/0022-fake-libvirt-and-fake-k8s.md) fakes over cluster | `dev/harness/fakek8s/` |
| [integration-harness](integration-harness.md) | [0022](../decisions/0022-fake-libvirt-and-fake-k8s.md), [0024](../decisions/0024-scenario-based-testing.md) | `integration/` — `TestEnv` (fake-k8s) + `envtestEnv` (real apiserver) |
| [lifecycle-traces](lifecycle-traces.md) | [0023](../decisions/0023-fake-libvirt-real-xdr.md) golden traces | `traces/` |
| stepping scheduler | [0025](../decisions/0025-deterministic-stepping-deferred.md) *(deferred)* | `dev/harness/step/` |

> **Stepping / deterministic interleaving is deferred.** A `step.Scheduler` seam
> exists (`dev/harness/step/scheduler.go`) and an orphaned stepping harness
> (`dev/harness/testharness/`), but **the production operator emits no step points**
> and the integration suite does not use them — it is polling-based. Treat the
> stepping machinery as a provisional, unwired experiment; see
> [ADR 0025](../decisions/0025-deterministic-stepping-deferred.md). It is
> intentionally *not* documented here as if it were usable.
