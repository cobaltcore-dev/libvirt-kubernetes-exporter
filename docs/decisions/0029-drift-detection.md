<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0029 — Drift detection via controller-runtime reconciler

Status: accepted · Date: 2026-08 · Updated: 2026-09 · Provenance: originally
reconstructed post-hoc on 2026-08-14 from code (`pkg/drift/detector.go`,
`integration/drift_test.go`) + `dev/feat-advanced-notes/TODO2.md`. Updated
2026-09 to reflect the settled implementation: the open question (periodic-LIST
vs controller-runtime) was resolved in favour of controller-runtime, and the
`DriftDetectionController` is now wired into `operator.Run`.

## Context

The CRDs we write can drift from what we last wrote: something (a human, another
controller, a bug) edits or deletes a `LibvirtDomain` object, or an object
exists in Kubernetes that the tracker has no record of. Because we only own the
status subresource we want to notice divergence and restore our truth without
paying for deep field diffing on every pass.

The original approach was a standalone `drift.Detector` running a periodic LIST
ticker. That record noted, honestly, that drift detection is a *classical
controller pattern* — subscribe to changes and reconcile — which is exactly what
controller-runtime is designed for, unlike the heartbeat write path where we
deliberately keep writes hand-rolled for throughput control
([[0021-optimistic-k8s-writes]], [[0027-custom-logging]]). The open question was
whether to accept controller-runtime's hidden network behaviour for this path.
The answer is yes for slow, event-driven objects; no for fast heartbeats (see
[[0034-controller-runtime-selective-adoption]]).

## Decision

Drift detection is implemented as a `DriftDetectionController`
(`internal/controllers/drift_detection_controller.go`) using controller-runtime's
reconciler interface, wired into the manager in `operator.Run`. On each reconcile
the controller:

- **CRD missing, domain tracked and old enough** → recreates the CRD from
  tracker state and registers the new UID with the invalidator so heartbeat
  picks it up.
- **CRD missing, domain recently seen** → requeues with `driftDetectionDelay`
  (grace period for the domain-write controller to create it first).
- **CRD missing, domain gone** → no-op; deletion was intentional.
- **CRD missing, domain not tracked** → no-op.
- **CRD present, tracker not ready** → requeues with a short delay.
- **CRD present, domain not tracked** → marks the CRD gone with `deletedAt` and
  `StateGone`/`PotentiallyCorrupted`; retention then handles physical deletion.
- **Label drift detected** → corrects labels in place.
- **Status drift detected** → corrects status by re-rendering from tracker state.

The controller watches `LibvirtDomain` objects so corrections are event-driven,
not on a fixed ticker; controller-runtime's built-in requeue and backoff handle
transient failures.

## Consequences

Drift is caught and repaired event-by-event at the cost the object's watch stream
already pays — no separate ticker or manual LIST. The controller-runtime informer
cache (which we previously avoided) is acceptable here because drift detection is
slow-moving and the objects are already cached for the watch; it would only be a
problem on the fast heartbeat path. The split is explicit: controller-runtime
manages domain CRDs and retention; the heartbeat CRD stays on the hand-rolled
dynamic-client path ([[0021-optimistic-k8s-writes]]). Corrections are safe
because we only write status, and the new-CRD grace period avoids false-positive
recreation races with the domain-write controller.

## Alternatives rejected

- **Periodic-LIST ticker (`drift.Detector`).** The original first-attempt
  implementation. Simpler and avoids controller-runtime's informer cache, but
  detection latency is the ticker interval and it requires manual LIST +
  resourceVersion compare. Now superseded — the controller pattern is the right
  fit for this problem, and the informer cost is acceptable for slow data CRDs.
- **Deep field diffing.** Exact but expensive and schema-coupled; status
  re-render from tracker state is simpler and always correct.
- **Auto-delete orphans in the detector.** Orphan deletion remains in the
  retention controller's scope; the drift controller only marks `gone`, keeping
  destructive action in one place.
