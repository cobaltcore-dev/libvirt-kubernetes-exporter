<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Tilt Setup

The Tilt setup spins up a kind cluster with fake-libvirt running on the host, the socket mounted into the cluster via hostPath, and the operator deployed inside kind with live rebuild on code changes.

## Prerequisites

- kubectl
- tilt
- a local Kubernetes cluster (the examples use `kind`, but any local cluster works)

## First-time cluster setup

```bash
kind create cluster --name libvirt-dev
```

## Start the dev environment

```bash
tilt up
```

Tilt will:
1. Start `fake-libvirtd` on your host at `/tmp/fake-libvirt/libvirt.sock`
2. Build the operator image and load it into kind
3. Apply CRDs, RBAC, and the operator Deployment
4. Hot-reload the operator image on any Go file change

## Inspect CRDs while running

```bash
kubectl get libvirtdomains
kubectl get libvirtdomainheartbeats
```

## Manage domains at runtime

The fake-libvirt API is available at `http://localhost:8081`.

**List domains:**
```bash
curl http://localhost:8081/api/domains
```

**Create a domain:**
```bash
curl -X POST http://localhost:8081/api/domains \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-vm","state":"running","maxMemKB":1048576,"memoryKB":524288,"vcpus":2}'
```

**Change domain state:**
```bash
curl -X PUT http://localhost:8081/api/domains/{uuid}/state \
  -H 'Content-Type: application/json' \
  -d '{"state":"shutoff"}'
```

Valid states: `running`, `shutoff`, `paused`, `crashed`, `shutdown`, `blocked`, `pmsuspended`

**Delete a domain:**
```bash
curl -X DELETE http://localhost:8081/api/domains/{uuid}
```

## Tear down

```bash
tilt down
kind delete cluster --name libvirt-dev
```
