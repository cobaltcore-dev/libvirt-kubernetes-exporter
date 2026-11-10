<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0028 — Span tracing + timeline web UI for knowledge transfer (aspirational)

Status: partial / aspirational · Date: 2026-08 · Provenance: reconstructed
post-hoc on 2026-08-14 from artifacts + recollection; evidenced by commits
`b2b2992`, `308f10e`, `c20c62b`, the `TRACING*.md` notes, and recollection that a
fuller version lives unfinished on a separate branch.

## Context

Distributed-systems glitches are hard to *explain*, not just to fix. Colleagues
may not design parallel systems, and the failure modes this operator
guards against (poll/event races, congestion, reappearing VMs) are unintuitive
until you watch them unfold. A visual timeline of events across the components
would build awareness far better than prose — the motive is knowledge transfer as
much as debugging.

## Decision

Instrument the components with span tracing and expose it, plus live logs, through
a web UI that draws a timeline of what happened across observer, tracker, exporter,
and the fakes. To feed it live, add a REST API and SSE (server-sent events) with a
log-capture ring buffer to the components. This is pursued as an *aspirational*
capability: a fuller version was attempted in an unsupervised session on a
separate branch (`feat/timeline` in a sibling checkout) and is **not complete**.

## Consequences

When it works, it turns scenarios ([[0024-scenario-based-testing]]) into a
teaching artifact — you can *see* the glitch — which is the main justification. The
cost is real and worth flagging: REST + SSE instrumentation across *all*
components is heavy for what is fundamentally dev tooling. A side benefit is that
the fake libvirt could graduate into a standalone project — useful for testing
other libvirt clients (e.g. Nova) without a fat host
([[0023-fake-libvirt-real-xdr]]) — though today it only serves our local
integration tests. Because this is branch-parked and partial, it is recorded as
intent-with-status, not as shipped architecture; a reader should not assume the
timeline UI is present or maintained.

## Alternatives rejected

- **Plain logs only.** Sufficient for an expert debugging alone, but poor at
  conveying interleavings to colleagues — which is the actual goal here.
- **A heavier external tracing/observability stack.** More than this dev-tooling
  purpose warrants; the lightweight in-repo tracing + SSE was chosen, and even
  that is flagged as heavy.
