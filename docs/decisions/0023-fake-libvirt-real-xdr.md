<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0023 — Fake libvirt speaks real XDR over a Unix socket

Status: accepted · Date: 2026-08 · Provenance: reconstructed post-hoc on
2026-08-14 from artifacts + recollection; evidenced by the `fake-libvirt` and
`trace-golden` openspec proposals.

## Context

Having chosen to test against a fake libvirt ([[0022-fake-libvirt-and-fake-k8s]]),
the question is *where* to insert it. The operator talks to libvirt over a Unix
socket using libvirt's XDR wire protocol (via `go-libvirt`). We could fake at the
Go-client boundary (swap the library for a mock), or fake at the wire boundary
(a process that actually speaks XDR on a socket). A goal beyond this operator
shaped the choice: we want the test infrastructure to be *minimally intrusive to
the operator* — ideally the operator under test is a black box we could swap out,
reusing the surrounding fakes and harness for a different consumer entirely.

## Decision

Implement the fake as a daemon (and embeddable library) that speaks real XDR over
a Unix socket, so the operator connects to it exactly as it would to real
libvirt, unmodified. To keep the fake honest, capture *golden traces* from real
libvirt instances (via the socket proxy) and use them to discover the actual RPC
sequences, required procedures, and error-tolerance behaviour that documentation
alone missed; the golden traces both guide the implementation and serve as
regression references.

## Consequences

The operator's libvirt code path is exercised for real — XDR framing,
connection lifecycle, procedure handling — with no test-only seam inside it, which
keeps the door open to reusing the harness against a different operator or even
testing other libvirt clients (e.g. Nova) without a fat host
([[0028-tracing-and-timeline-ui]] notes the same standalone potential). The cost
is implementing enough of the XDR protocol and the right procedure set — real
effort, and the reason golden traces exist (they revealed a mandatory
auth/feature-check preamble and several procedures we would not have guessed).
Whether full operator-swappability is actually achievable or needed remains an
open question; the XDR choice keeps it possible without committing to it.

## Alternatives rejected

- **Mock the `go-libvirt` client API in-process.** Simpler to write, but couples
  tests to client internals, cannot exercise the real wire path, and bakes a
  test seam into the operator — the opposite of the minimal-intrusion goal.
- **Only run the standalone fake daemon (no embeddable library).** Fine for
  `virsh`-style checks, but the harness needs to manipulate domain state and
  inject events programmatically from inside tests.
