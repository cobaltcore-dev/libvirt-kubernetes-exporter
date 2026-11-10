<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0022 — Fake libvirt and fake Kubernetes instead of a real cluster

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; evidenced by `requirements/DESIGN.md`,
the `testing-harness`/`fake-libvirt` openspec proposals, and recollection of the
primary driver.

## Context

The behaviour that matters here is glitch-free conduct under adverse conditions —
socket hangs, missing events, congestion toward the API server, k8s
inconsistencies — the very conditions a real end-to-end setup produces only
rarely and cannot reproduce on demand. Standing up the real thing means spawning
and migrating actual VMs through OpenStack against a live QA cluster; that yields
excellent *truth* but exercises very little of our failure code paths, and it is
slow.

The dominant driver, stated plainly, is round-trip time to a failure. Our
department's build-and-deploy loop is punishing: a typo can cost a full
build+deploy cycle plus double-approval on a protected branch before you even see
the second run. This loses the ability to iterate quickly. A test setup
that runs entirely on a laptop — and is amenable to AI-agent-driven iteration —
is worth more day-to-day than fidelity we can only get end-to-end.

## Decision

Test against *fakes* rather than a real cluster: a fake libvirt and a fake
Kubernetes API server that the operator talks to unmodified. This lets tests
manufacture failure modes (hangs, errors, delays, inconsistencies) deterministically
and run locally in seconds. A Tilt setup (`Tiltfile`, `dev/tilt-setup`) currently provides a complementary *supported* local dev mode against a local
Kubernetes — good for iterating on a running operator, but without the harness's
deterministic failure-injection. End-to-end testing against real OpenStack/libvirt
still happens, but as a truth check, not the inner loop.

## Consequences

Failure-path coverage and iteration speed rise sharply: scenarios that are
near-impossible to stage on QA become ordinary unit/integration tests
([[0024-scenario-based-testing]]), and the whole thing runs without deployment.
The fakes must track real libvirt behaviour closely enough to be trustworthy,
which is why they are grounded in captured golden traces
([[0023-fake-libvirt-real-xdr]]). The cost is maintaining two fakes and the risk
that a fake diverges from reality; the end-to-end truth check exists to catch
that. The fake-k8s choice specifically enables injecting the k8s errors that
[[0021-optimistic-k8s-writes]] must survive.

## Alternatives rejected

- **A real (kind or QA) cluster for the inner loop.** Hard to induce controlled
  congestion and failures, slow, and — for QA — gated behind the deploy/approval
  loop we are trying to escape. (A local Tilt-based cluster is nonetheless kept as
  a *supported complement* for iterating on a running operator, just not the
  failure-mode inner loop.)
- **Mock only the Kubernetes Go client, keep real libvirt.** Leaves the libvirt
  side (the flakier one) untestable for failure modes and couples tests to client
  internals ([[0023-fake-libvirt-real-xdr]] rejects the analogous libvirt-client
  mock).
