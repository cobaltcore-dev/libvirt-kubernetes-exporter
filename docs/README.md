<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Docs for the Libvirt-Kubernetes-Exporter

The `docs/` tree is the source of *intent* (the code is the source of *behaviour*):

| Document | Purpose |
|----------|---------|
| [`docs/decisions/`](docs/decisions/) | Architecture Decision Records (why + rejected alternatives); start at the README's index and decision graph |
| [`docs/reference/crd-schema.md`](docs/reference/crd-schema.md) | The CRD schema: `LibvirtDomain` + `LibvirtDomainHeartbeat`, states, labels |
| [`docs/testing/`](docs/testing/) | How the test infrastructure works: fake-libvirt, fake-k8s, harness, traces |
