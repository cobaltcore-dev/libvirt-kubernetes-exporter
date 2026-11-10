<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Golden traces & lifecycle capture

Executes [ADR 0023](../decisions/0023-fake-libvirt-real-xdr.md) (golden traces
ground the fake in real libvirt behaviour).

Golden traces were captured from a real libvirt (via the socket proxy) to discover
the actual RPC sequences and to serve as regression references for the fake. Two
kinds:

- **Point-in-time** traces (`virsh version/list/dominfo/dumpxml`) — the auth
  preamble + per-command cores.
- **Lifecycle-event** traces — asynchronous events across real VM state
  transitions.

## Reality notes (so the golden data isn't misread)

- The lifecycle event procedure is **`DOMAIN_EVENT_CALLBACK_LIFECYCLE`**, not
  `DOMAIN_EVENT_LIFECYCLE`.
- Only **5 of 8** captured segments actually contained a lifecycle event
  (`01-create`, `03-start-openstack`, `05-start-virsh`, `06-destroy-virsh`,
  `07-start-virsh`). `02-stop-openstack` and `04-shutdown-virsh` captured only the
  event-registration handshake and **no lifecycle event** — a real observation
  about event leakiness on stop transitions, and exactly the kind of gap
  [ADR 0014](../decisions/0014-dual-source-observation.md) (poll corrects for
  missed events) exists to handle.
