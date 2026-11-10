<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0026 — Separate `dev/` tooling from the production operator

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; evidenced by the repo layout and commit
`4901a7b` ("Move dev/test tooling under dev/ to separate production from
infrastructure").

## Context

The testing effort brings a lot of code that is *not* the operator: the fake
libvirt, the fake k8s server, the harness, the socket proxy, trace tooling, and
later a web UI. Left intermixed with the operator, this tooling would blur what
actually ships, drag test-only dependencies into the production module, and make
it harder for a reader (or an auditor) to see the small thing that runs in prod.

## Decision

Keep production and infrastructure in separate trees: the operator and its
packages are the shipping artifact; everything test/dev — fakes, harness, proxy,
traces, web UI — lives under `dev/`. Module boundaries reinforce it, so the
production build does not pull in test-only code.

## Consequences

The production surface stays legible and its dependency set stays lean; the
tooling can grow freely under `dev/` without contaminating what ships. This also
makes the "reuse the harness against a different operator" idea cleaner
([[0023-fake-libvirt-real-xdr]]), since the infrastructure is already decoupled
from this particular operator. The cost is some cross-module wiring (e.g. the
harness importing the operator's naming package) and the discipline to keep the
boundary honest as tooling expands.

## Alternatives rejected

- **One module/tree for everything.** Simpler imports, but test-only dependencies
  leak into the production build and the shipping artifact is harder to see.
