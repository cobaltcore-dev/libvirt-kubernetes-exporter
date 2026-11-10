<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0027 — Roll our own `slog`-based logging instead of controller-runtime logging

Status: accepted · Date: 2026-08 · Updated: 2026-09 · Provenance: reconstructed
post-hoc on 2026-08-14 from artifacts + recollection; the controller-runtime pain
is recollection from a sibling operator, evidenced by commits `cff3765`/`2058ebd`.
Updated 2026-09: the predicted "two logging idioms" risk is resolved — see
Consequences.

## Context

This is a parallel system, and when debugging it we need to see *which goroutine
did what, in what order*. In a sibling operator built on controller-runtime,
logging actively got in the way: stack traces surfaced inside the runtime rather
than at the real fault, so the logs pointed at the framework instead of our bug.
We also wanted simple, controllable output — including per-component colour — to
follow concurrent activity by eye, and control over log level.

## Decision

Use Go's standard `slog` directly, wired through a small internal logging package,
rather than controller-runtime's logging conventions. Log lines carry
component/level context; output and verbosity are ours to control. This is one
instance of a broader stance of not adopting controller-runtime's hidden machinery
without a reason ([[0021-optimistic-k8s-writes]] declines its read-through cache
for the same "know when it talks to the network" reason).

Where controller-runtime is selectively adopted for appropriate paths (see
[[0029-drift-detection]]), bridge its internal logger to the same slog pipeline
via `ctrl.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))` rather than
letting it emit a separate log stream. Our own code still uses slog directly; the
bridge only covers controller-runtime's internal cache/reconcile/watch output.

## Consequences

Logs point at our code and read in a way that suits a concurrent, multi-goroutine
pipeline; level control and formatting are in our hands, and there is one less
framework behaviour to second-guess while debugging races. The predicted cost —
"if we later adopt that framework… two logging idioms to reconcile" — is resolved:
the `logr.FromSlogHandler` bridge means controller-runtime's internal logs flow
through the same slog pipeline at the same level, so there is one stream regardless
of which path generated a log line.

## Alternatives rejected

- **controller-runtime's logging.** Buried real faults under runtime stack traces
  in a sibling operator and brings framework conventions we did not want here; the
  concurrency-debugging need argued for direct control.
