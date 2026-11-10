<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0001 — `crdmeta` as the single source of truth for CRD naming

Status: accepted · Date: 2026-08

## Context

CRD naming metadata (group, version, kinds, plural resource names) was
duplicated across the operator, the test harness, the fake-k8s server, and the
integration tests as hardcoded string literals (`"kvm.cloud.sap"`,
`"libvirtdomains"`, …). We were about to make the plural names *configurable at
runtime* (see 0004), which meant every one of those hardcoded copies would have
to resolve the same suffix consistently — an invitation to drift. The harness
and test packages need only the *naming*, not the k8s types, so the source of
truth had to be importable without pulling k8s dependencies into those leaf
modules.

## Decision

Introduce `operator/pkg/crdmeta` (later `pkg/crdmeta`) as the single source of
truth: a tiny package depending only on `os`. Group/version/kinds are consts;
the plural resource names are exposed as **functions** (`LibvirtDomainResource()`
etc.) that return `base + Suffix`, where `Suffix` is read once from the
`CRD_SUFFIX` env var. Everything — operator, harness, fake-k8s, integration
tests — imports `crdmeta` instead of hardcoding strings.

## Consequences

Any component computes the correct (possibly suffixed) name from one place. The
harness module had to take a dependency on the operator module to import
`crdmeta`, which is acceptable for a zero-k8s-dep leaf package. Because the
suffix is a runtime value, the resolved names *must* be functions/vars, not
consts — callers changed from `types.LibvirtDomainResource` to
`types.LibvirtDomainResource()`. One irreducible duplication remains: the base
plural lives both here and in the controller-gen marker (see 0003), because
controller-gen cannot read Go consts and the operator cannot read markers.

## Alternatives rejected

- **Compile-time naming via `-ldflags` build vars.** Would make the names consts
  again, but forces a rebuild per variant and couples naming to the build system;
  rejected in favour of a runtime env var that a single image can honour.
- **Leave strings hardcoded, sed everything.** The sed surface would span Go
  source, tests, and YAML — far more fragile than one importable package.
