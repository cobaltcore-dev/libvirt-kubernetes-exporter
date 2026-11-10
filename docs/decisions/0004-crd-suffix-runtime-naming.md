<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0004 — Per-deployment CRD isolation via runtime `CRD_SUFFIX` + pipeline sed

Status: accepted · Date: 2026-08 · Depends on: 0001, 0003, 0009

## Context

Configurable per-branch dev deployments (0009) require each deployment on the
shared QA cluster to write its *own* CRDs. CRDs are cluster-scoped, so
namespacing can't isolate them — the *names* must differ per deployment. The
operator resolves CRD names from `crdmeta` (0001), and the CRD YAML is generated
with a fixed base name (0002/0003), so the suffix has to be injected in two
places that must agree: the running operator and the installed CRD manifests.
This ADR records the *how* for the CRD surface; 0009 is the *why*.

## Decision

Introduce a `CRD_SUFFIX` env var. `crdmeta.Suffix` reads it once at startup, so
`LibvirtDomainResource()` returns `libvirtdomains<suffix>`. The Helm chart
exposes `crdSuffix`, passes it as `CRD_SUFFIX`, and templates the RBAC
ClusterRole `resources:` from the same value so permissions match. The CRD
*manifests* are postfixed by a `sed` step in the pipeline's build-chart job,
which rewrites the generated `/crds` YAML before packaging. A leading `-` in the
suffix (e.g. `-shared`) is stripped for the CRD name segment (→
`libvirtdomainsshared`), since CRD names can't start with a dash, while the
operator env keeps the raw value.

## Consequences

Each dev deployment gets isolated CRDs (`libvirtdomainsshared`, …) and matching
RBAC, from one image + one chart, selected purely by values. The sed step must
rename the longer plural first (`libvirtdomainheartbeats` before
`libvirtdomains`) because the former has the latter as a prefix — otherwise the
suffix double-applies. Helm never templates `/crds`, so suffixed CRDs don't
appear in a `helm diff` and must be checked with `kubectl get crd`; and `/crds`
are install-once and never removed, so torn-down dev deployments leak their
suffixed CRDs (a cleanup job is deferred).

## Alternatives rejected

- **Compile-time / per-variant images.** Rejected with 0001 — a single image
  honouring a runtime env var is the whole point.
- **Template the CRDs through Helm (move them out of `/crds`).** Would let Helm
  apply the suffix uniformly, but loses Helm's install-once CRD safety and its
  ordering guarantees; keeping `/crds` + sed preserves that behaviour.
