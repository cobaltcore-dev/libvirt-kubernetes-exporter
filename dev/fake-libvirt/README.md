<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# fake-libvirt

A fake libvirt daemon for testing. It speaks the libvirt RPC protocol over a
Unix socket, so real clients (virsh, go-libvirt) can connect to it without a
hypervisor.

## What it does

- Accepts connections on a Unix socket
- Handles the subset of libvirt procedures needed by the exporter and virsh
- Ships with three pre-loaded test domains (running, shutoff, paused)
- Returns proper libvirt error codes (e.g. `VIR_ERR_NO_DOMAIN`) for unknown domains

## Running

```bash
go run ./cmd/fake-libvirtd/ --socket /tmp/fake-libvirt.sock
```

## Testing with virsh

```bash
# List all domains
virsh -c "qemu+unix:///system?socket=/tmp/fake-libvirt.sock" list --all

# Get domain info
virsh -c "qemu+unix:///system?socket=/tmp/fake-libvirt.sock" dominfo test-vm-running

# Lookup a non-existent domain (verifies error responses)
virsh -c "qemu+unix:///system?socket=/tmp/fake-libvirt.sock" dominfo nonexistent
```

## Controlling state at runtime

`FakeState` exposes three methods for use in tests:

```go
// Add a new domain
err := state.AddDomain(&state.Domain{
    UUID:      [16]byte{...},
    Name:      "my-vm",
    ID:        3,
    State:     state.DomainRunning,
    MaxMem:    2097152,
    Memory:    1048576,
    NrVirtCPU: 2,
})

// Remove a domain by UUID
err := state.RemoveDomain(uuid)

// Change a domain's state
err := state.SetDomainState(uuid, state.DomainShutoff)
```

State constants: `DomainRunning`, `DomainShutoff`, `DomainPaused`,
`DomainBlocked`, `DomainShutdown`, `DomainCrashed`, `DomainPmsuspended`.

## Running the unit tests

```bash
go test ./...
```
