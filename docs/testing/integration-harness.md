<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Integration harness

Executes [ADR 0022](../decisions/0022-fake-libvirt-and-fake-k8s.md) (fakes over a
real cluster) and [ADR 0024](../decisions/0024-scenario-based-testing.md)
(scenario testing). Grounded in `integration/` (verified 2026-09-02 against
`integration_test.go`, `envtest_*.go`).

Two independent harnesses live in the `integration/` package:

| Harness | Transport | Purpose |
|---------|-----------|---------|
| `TestEnv` (`integration_test.go`) | fake-libvirt socket + fake-k8s HTTP | Fault injection, congestion, tracing |
| `envtestEnv` (`envtest_env_test.go`) | fake-libvirt socket + real kube-apiserver (envtest) | Controller lifecycle, drift correction, retention, heartbeat owner refs |

---

## `TestEnv` (`integration/integration_test.go`)

> **What the harness actually is:** a **polling-based** `TestEnv` that wires
> fake-libvirt + fake-k8s + the operator + tracing, and asserts by waiting for
> resources to appear/change. It uses `operator.NewWithExporter` which bypasses
> the controller-runtime manager entirely — the three controllers
> (`DomainWriteController`, `DriftDetectionController`, `DeleteRetentionController`)
> do **not** run in this harness.

```go
env := NewTestEnv(t)                 // or NewTestEnvWithName(t, name) for trace output
defer env.Close()                    // removes tmp dir only if TRACE_OUTPUT_DIR set (else kept for traces)

env.State()                          // *fakelibvirt.FakeState — drive libvirt side
env.FakeK8s                          // *fakek8s.Server — assert/inject on k8s side
env.SocketPath()                     // unique per-test unix socket
errCh := env.RunOperator()           // go operator.Run(ctx); cancel via Close()
```

### Waiting / assertions (polling, ~100ms interval)

```go
env.WaitForResource(group, ver, res, ns, name, timeout)
env.WaitForResourceCount(..., n, timeout)
env.FakeK8s.GetResource(...); env.FakeK8s.Requests()
```

Typical flow: `env.State().AddDomain(...)` → `env.RunOperator()` →
`env.WaitForResource(...)`.

### Fault-injection scenarios

`scenario_test.go` drives adverse conditions via
`env.FakeK8s.SetFault(pathPattern, &fakek8s.FaultConfig{Code, ErrorMsg, Delay})`
— k8s errors, delays, reconnection, high-volume congestion. See
[fake-k8s](fake-k8s.md) for the fault API.

### Tracing

`TestEnv` wires a Chrome-trace collector/writer (`SetTraceCallback` on Tracker and
Exporter). Set `TRACE_OUTPUT_DIR` to emit `TracePath()` traces consumed by the
timeline UI ([ADR 0028](../decisions/0028-tracing-and-timeline-ui.md)); backs
`tracing_test.go` and `throughput_test.go`.

---

## `envtestEnv` (`integration/envtest_env_test.go`)

Uses `sigs.k8s.io/controller-runtime/pkg/envtest` to start a real
`kube-apiserver` process against the CRD manifests in
`charts/libvirt-kubernetes-exporter/crds/`. This harness uses
`operator.NewWithRestConfig(cfg, restCfg)` which wires the full manager and all
three controllers.

### Prerequisites

Requires `KUBEBUILDER_ASSETS` to point at a directory containing the
`kube-apiserver` binary. Use `setup-envtest`:

```bash
# one-shot
make test-envtest

# or manually (cache the path for repeated runs)
export KUBEBUILDER_ASSETS=$(setup-envtest use 1.36 -p path)
go test -v -run TestEnvtest ./integration/
```

`setup-envtest` is installed automatically by `make test-envtest` if not present.
In CI, cache `~/.local/share/kubebuilder-envtest` between runs to avoid
re-downloading the binaries.

Tests are **skipped** (not failed) when `KUBEBUILDER_ASSETS` is unset, so the
normal `make check` / unit-test run is unaffected.

### Structure

```go
env := newEnvtestEnv(t)          // starts envtest + fake-libvirt + operator; t.Cleanup wired

env.FakeLibvirt.State()          // *fakelibvirt.FakeState — add/remove/change domains
env.DynClient                    // dynamic.Interface against the envtest apiserver

env.addDomain(t, uuid, name)     // add domain to fake-libvirt, returns UUID string
env.waitForDomain(t, uuid, timeout)
env.waitForDomainStatus(t, uuid, wantState, timeout)
env.waitForDomainGone(t, uuid, timeout)
env.waitForHeartbeat(t, uuid, timeout)
```

Retention is configured to 100 ms in `newEnvtestEnv` so retention-expiry tests
complete quickly.

### What it covers

| File | Scenarios |
|------|-----------|
| `envtest_domain_test.go` | Domain appears, state change, deletion lifecycle |
| `envtest_drift_test.go` | Status tampered externally → corrected; CRD deleted → recreated |
| `envtest_retention_test.go` | CRD deleted after retention window; running domain kept |
| `envtest_heartbeat_test.go` | Heartbeat CRD created; owner reference points at domain CRD UID |

