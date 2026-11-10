<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0010 — Export libvirt VM state to Kubernetes custom resources

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection (see `../../EXTRACT_ADRS_PROMPT.md`);
rationale is partly inferred, not contemporaneous.

## Context

We run a large OpenStack-based cloud that is migrating to a KVM
backend. At our scale, OpenStack's own database view of a VM is regularly
inconsistent with the VM that is actually running on the host — OpenStack is
complex, and the situation is worse in QA/dev because we run a heavily-evolving
cloud-hypervisor (CH) libvirt driver whose behaviour is still maturing. The
practical consequence is a real *distance* between "what the OpenStack DB thinks"
and "what libvirt on the host reports," and today the only trustworthy way to
close that gap is to SSH onto a host and ask `virsh` directly — which we do
routinely when debugging QA.

The requirement, then, is narrow and concrete: continuously expose the *current,
real* state of libvirt domains somewhere queryable, with a reliability
comparable to asking `virsh` yourself. The company is "all-in" on Kubernetes —
host lifecycle, onboarding/offboarding, and a forthcoming scheduling layer are
all operators consuming CRDs — so the natural sink for this state is a CRD other
operators and `kubectl` can already watch.

## Decision

Build an operator that observes libvirt (via its socket) and continuously exports
domain state as Kubernetes custom resources. Kubernetes is the distribution
mechanism: consumers watch/query CRDs with tooling they already have, and no new
client or API surface is introduced.

## Consequences

VM state becomes a first-class, watchable Kubernetes object usable by the
host-lifecycle operator, the future scheduling layer, and ad-hoc `kubectl`
queries — without SSH access to hypervisors. The cost is that we now own the
faithfulness problem end to end (see [[0016-time-slice-guarantee]],
[[0019-extended-states-and-retention]]) and inherit Kubernetes' eventual
consistency: subscribers see the *current* state, not a guaranteed stream of
every transition. Actual CRD update-rate limits in our environment are unknown
and remain unmeasured, which shapes later decisions ([[0017-heartbeat-data-crd-split]]).

## Alternatives rejected

- **Logging / metrics (OpenSearch, Prometheus).** Already in place, but gives
  history and aggregates, not a current structured state that operators can act
  on; loses the "is this VM running *right now*" question.
- **On-demand SSH / direct libvirt queries.** This is the status-quo pain we are
  escaping: susceptible to socket hangs at query time, needs host credentials,
  and is invisible to Kubernetes operators.
- **A custom REST API.** Would need bespoke clients and re-implement watch,
  caching, and RBAC that Kubernetes already provides; rejected against the
  all-in-Kubernetes reality.
