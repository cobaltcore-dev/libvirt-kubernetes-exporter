<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0009 — Configurable per-branch dev deployments on the shared QA cluster

Status: accepted · Date: 2026-08

## Context

Engineering policy mandates that our single QA region is the first stage of the
staggered (production) deployment. But that same region is also where developers
need to run integration tests against a live cluster. In other projects this
creates constant friction: developers must coordinate on the one shared instance
and route changes through GitHub approvals just to try something out. We wanted a
way to stand up a developer's own build on the live QA cluster — parametrized
mainly by the git branches it uses — without asking anyone for approval or
serializing behind other people's testing.

The obstacle is isolation. Namespaced resources (DaemonSet, ServiceAccount) can
be separated by putting each dev deployment in its own Kubernetes namespace. But
the operator also owns **cluster-scoped** resources — the CRDs and the
ClusterRole/Binding — which have no namespace and would collide across
deployments (and with the incumbent operator, 0003).

## Decision

Introduce a configurable *set* of dev deployments (an ERB list in the pipeline),
each keyed on the branches it tracks (operator / umbrella / values) and its
target cluster. Namespaced resources isolate via a per-deployment namespace; the
un-namespaced resources are isolated by a **hard renaming** driven by a
per-deployment suffix. This renaming artifice deliberately manifests in three
coordinated places: ad-hoc `sed` renaming of the *untemplated* CRD YAML in the
pipeline (0004), suffix templating of the ClusterRole/Binding names in the Helm
chart following the same pattern (0005), and runtime support for dynamically
named CRDs in the operator (0001, 0004).

## Consequences

A developer gets a private, live integration environment on QA by pointing a
dev-deployment entry at their branches — no approvals, no coordination on the
shared instance, while QA still serves its mandated role as staggered stage 1.
The cost is the three-place renaming machinery and the requirement that those
places stay in sync. This decision is the *why* behind 0004 (CRD suffix) and 0005
(cluster-scoped naming); those record the *how* for each surface.

## Alternatives rejected

- **Share the single QA instance and coordinate via approvals.** The status quo
  in other projects — the friction we set out to remove.
- **Namespace isolation alone.** Insufficient: cluster-scoped CRDs and RBAC don't
  live in a namespace and would still collide.
- **Ephemeral per-PR environments.** Heavier machinery than needed for 2–3
  long-lived per-developer deployments, and doesn't fit the "point it at my
  branch" model.
