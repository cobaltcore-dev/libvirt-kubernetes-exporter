<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0002 — Generate CRDs with the controller-gen CLI, not controller-runtime

Status: accepted · Date: 2026-08 · Depends on: 0001

## Context

The CRD YAML in the chart's `/crds` directory was hand-written and could drift
from the Go types that the operator actually marshals. We wanted the Go structs
to be the source of truth for the CRD *schema* (as `crdmeta` is for the *names*,
0001). The obvious tool is `controller-gen`, which normally arrives with the
controller-runtime / kubebuilder framework — but this operator deliberately uses
a hand-written dynamic client, not the controller-manager runtime, and we did not
want to drag that framework (and its reconcile loop, manager, scheme
registration) into the binary just to generate YAML.

## Decision

Use the **`controller-gen` CLI only** — installed via `go install` and invoked by
a `make generate` target — to generate CRD manifests from struct markers
(`+kubebuilder:resource:path=…`, `+groupName`, `+versionName`, printer columns).
The generated YAML is committed under the chart's `/crds`. The operator keeps its
hand-written dynamic client; controller-runtime is *not* a dependency.

## Consequences

Struct changes flow to CRD YAML via one `make generate` step; the markers are
inert comments at runtime (no new imports). The generated filenames encode the
plural (`kvm.cloud.sap_libvirtdomains.yaml`), so a rename changes filenames —
old ones must be removed on regeneration. `controller-gen object` (DeepCopy) is
available but unused, since the dynamic client doesn't need typed deepcopies.

## Alternatives rejected

- **Full controller-runtime / kubebuilder scaffolding.** Rejected: it would
  restructure the operator around a manager/reconciler it doesn't use, for no
  benefit beyond the codegen we can get from the CLI alone.
- **Keep hand-writing the CRD YAML.** Rejected: perennial drift risk between the
  YAML schema and the Go structs the operator serializes.
