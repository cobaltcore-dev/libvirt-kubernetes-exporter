<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Integration Tests

This directory contains the full test suite for the operator. It is a standalone Go module with `replace` directives pointing to sibling packages in the monorepo.

## Running the tests

**Unit tests** — no setup required, always run:

```bash
cd integration
go test -v
```

These cover heartbeat throttling, config loading, rate limiting, poll rate calculation, and the Chrome Trace Format output.

**Integration tests** — require a real Kubernetes API server via [envtest](https://book.kubebuilder.io/reference/envtest):

```bash
make test-envtest
```

This installs `setup-envtest` if needed, downloads Kubernetes 1.36 binaries on first run (cached in `~/.local/share/kubebuilder-envtest`), and runs all `TestEnvtest_*` tests. These spin up a real `kube-apiserver` + `etcd` in-process alongside a fake libvirt, and test the full operator pipeline end-to-end: domain lifecycle, heartbeats, drift detection, migration detection, and delete retention.

To run them manually:

```bash
ASSETS=$(setup-envtest use 1.36 -p path)
KUBEBUILDER_ASSETS="$ASSETS" go test -v -count=1 -timeout 120s . -run TestEnvtest
```

> The envtest harness loads CRD schemas from `../charts/libvirt-kubernetes-exporter/crds`. If you change the API types, regenerate them first with `make generate`.

## Interactive demo

The `cmd/demo/` binary runs the operator against fake-libvirt and fake-k8s, with a web UI for inspecting state and running tests interactively:

```bash
cd integration/cmd/demo
go run . --webui-dir=../../dev/webui
# Open http://localhost:8000
```

## Note on skipped tests

Several tests (`TestBasicDomainDiscovery`, `TestStateChange`, `TestScenario_*`, `TestThroughput_*`) are currently skipped. They used an older fake-k8s HTTP harness that predates the controller-runtime migration and are preserved for future fault-injection work.
