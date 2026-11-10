<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0037 — Restore fault injection after controller-runtime adoption

Status: open / temporary mitigation accepted · Date: 2026-09 · Depends on: 0022, 0024, 0034, 0036

## Context

The pre-controller-runtime operator had two constructors, but not two product
modes. `New` built a Kubernetes client from kubeconfig or in-cluster config;
`NewWithExporter` let tests inject a `dynamic.Interface` pointed at fake-k8s.
That split was a setup seam, not a decision to exercise less Kubernetes API. The
small fake-k8s surface was possible because the old exporter itself made a small,
explicit set of dynamic-client CRUD calls.

With selective controller-runtime adoption ([[0034-controller-runtime-selective-adoption]]),
domain CRD writes, drift correction, and retention now run under a manager. That
manager uses more Kubernetes API machinery than fake-k8s currently implements:
list/watch streams, informer cache startup, resource versions, status
subresources, patch semantics, UIDs, and Kubernetes-shaped errors. Heartbeat
writes deliberately stay on the hand-rolled dynamic-client path.

The important requirement to preserve is fault injection at the Kubernetes API
boundary: returning failures, adding latency, and observing raw write behaviour.
Plain envtest gives real controller-runtime semantics, but not those
fault-injection and request-counting hooks. The existing fake-k8s harness has the
right kind of instrumentation, but cannot currently be used as the operator's API
server once controller-runtime is in the path unless it is extended or combined
with another mechanism.

## Decision

For this branch, use envtest as the correctness boundary for controller-runtime
behaviour: domain CRD writes, drift detection, retention, status subresources,
watch/reconcile, and heartbeat owner-reference handoff.

Keep the fake-k8s tests in source, but skip the operator-level fake-k8s harness
tests while the branch migrates to controller-runtime. The skipped tests preserve
the scenarios and instrumentation requirements for later work.

The follow-up design is intentionally open. The preferred direction is to extend
fake-k8s enough to support the controller-runtime path while keeping its
fault-injection controls. If that turns out too large, alternatives include a
fault-injection proxy in front of envtest or another targeted failure-injection
seam. A split endpoint model — fake-k8s for heartbeat CRDs and envtest for data
CRDs — is not the current target because owner references, UIDs, and API-object
relationships cross that boundary.

Do not interpret `NewWithExporter` as a supported second operator runtime. It is
a legacy injection seam from the old hand-written exporter path and is currently
incomplete for domain CRD writes.

## Consequences

Normal lifecycle tests can be covered by envtest today. If fake-k8s is later
extended far enough to run the controller-runtime path, the project still needs a
separate decision about test topology: use envtest as the single correctness
backend, switch back to fake-k8s as the single instrumented backend, or make a
subset of lifecycle tests runnable against both.

Running the same lifecycle tests against both envtest and fake-k8s would validate
the fake against a real API-server baseline and reduce the chance that either
backend hides a bug. The cost is extra harness abstraction, more complicated test
setup, longer test runtime, and renewed pressure on the `NewWith*` operator
constructors to support multiple Kubernetes endpoint shapes without becoming
separate operator modes.

Choosing one backend for ordinary lifecycle tests keeps the tests and
constructors simpler, but then fake-k8s needs its own focused validation strategy
if it remains the fault-injection backend. Otherwise a bug in fake-k8s could
invalidate the very failure-mode tests it is meant to support.

Fault-injection, artificial latency, raw request logging, and write-count based
throughput tests cannot be ported directly to plain envtest. They remain visible
as skipped tests so the project does not lose the scenarios while accepting the
controller-runtime change.

The main risk is losing coverage of HTTP-level failure behaviour until a new
instrumentation strategy exists. The benefit of the temporary skip is avoiding a
second domain-write implementation solely to keep the legacy harness passing,
which would recreate the code-path duplication the fake boundaries were meant to
avoid.

## Alternatives rejected

- **Delete the fake-k8s tests.** This would make the fault-injection and request
  instrumentation requirements disappear into git history even though they are
  still desired.
- **Comment out the tests.** Skipped tests remain compiled, named, and visible in
  `go test -v`; commented-out code does not.
- **Keep `NewWithExporter` working by adding a legacy domain writer.** That would
  restore the old harness quickly, but at the cost of a second domain-CRD write
  path that can drift from `DomainWriteController`.
- **Require fake-k8s to emulate controller-runtime immediately.** Extending
  fake-k8s is the preferred follow-up direction, but doing it before merging this
  branch is larger than the branch itself: it needs watch/list/status and
  API-machinery semantics, not just CRUD storage.
- **Decide now that every portable lifecycle test must run against both envtest
  and fake-k8s.** This may become the right validation shape once fake-k8s is
  controller-runtime-capable, but deciding it now would force new harness and
  constructor abstractions before the fake-k8s feasibility work is done.
- **Decide now that one backend permanently replaces the other.** Envtest is
  complete enough for most correctness tests, and an extended fake-k8s might be
  attractive for instrumented tests, but the final split depends on how hard it is
  to extend fake-k8s or instrument envtest. That research is still pending.
