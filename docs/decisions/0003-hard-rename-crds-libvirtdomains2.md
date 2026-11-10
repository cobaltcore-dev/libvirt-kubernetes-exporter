<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0003 — Hard-rename base CRDs to `libvirtdomains`

Status: accepted · Date: 2026-08 · Depends on: 0001, 0002

## Context

The rewrite defined CRDs named `libvirtdomains` / `libvirtdomainheartbeats`. The
*previous* implementation, currently running on our single QA cluster (the same
cluster we test on, 0009), owns CRDs named `libvirtdomains` /
`libvirtdomainxmls`. CRDs are cluster-scoped, so the rewrite's `libvirtdomains`
would clash with the incumbent's — the two operators would fight over one CRD
definition. We needed a distinct base name so the rewrite can run *alongside* the
old operator while we bring the rewrite to an acceptable alpha; once the old
variant is shut off, the base name can be back-renamed.

## Decision

Hard-rename the base plurals to `libvirtdomains` / `libvirtdomainheartbeats`.
The name is changed in the two synced locations (0001): the `Base*Resource`
consts in `crdmeta` and the `+kubebuilder:resource:path=` markers in the types,
followed by `make generate` to regenerate the YAML.

## Consequences

The rewrite no longer collides with the incumbent CRDs, so both can run on the QA
cluster during the alpha period. A cross-cutting "HARD-RENAME SYNC POINT" comment
was added at both locations, because controller-gen reads the marker and the
operator reads the const — neither can read the other, so a future rename (e.g.
the eventual back-rename after the old variant is retired) must touch both.
Choosing the `2` suffix (over a hyphenated form) also sidesteps a Kubernetes
constraint that CRD plural names must be lowercase-alphanumeric with no dashes.

## Alternatives rejected

- **`libvirtdomains-new` / `-v2` (hyphenated).** Rejected: Kubernetes rejects
  dashes in the resource plural (it becomes part of the API path and
  `metadata.name`).
- **`libvirtdomainsv2`.** Viable and reads as a version bump. It doesn't change
  the prefix hazard either way: the hazard is that the singular stem
  `libvirtdomain` is a prefix of `libvirtdomainheartbeat`, so *any* shared-stem
  scheme (`libvirtdomains` / `libvirtdomainsv2` / …) leaves `libvirtdomains` a
  prefix of `libvirtdomainheartbeats`, which the sed step in 0004 must handle by
  renaming the longer plural first. `2` was chosen as the terser option.
- **Keep `libvirtdomains`, uninstall the old operator first.** Rejected: forces a
  destructive, coordinated cutover on the shared QA cluster instead of allowing
  coexistence during the alpha.
