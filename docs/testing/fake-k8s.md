<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Fake Kubernetes API server

Executes [ADR 0022](../decisions/0022-fake-libvirt-and-fake-k8s.md) (test against
a fake k8s so failure modes are injectable). Grounded in `dev/harness/fakek8s/`
(verified 2026-08-14 against `server.go` and `api.go`; the former openspec
proposal had drifted — this page reflects the code).

## Core server (`fakek8s.Server`)

```go
srv := fakek8s.New()          // auto-starts an httptest.Server
url := srv.URL()              // base URL for the operator's client
srv.Close()                   // returns nothing (not error)
```

Serves dynamic CRD paths for both namespaced
(`/apis/{group}/{version}/namespaces/{ns}/{resource}[/{name}]`) and
**cluster-scoped** (`/apis/{group}/{version}/{resource}[/{name}]`) resources. The
core `/api/v1/` path is intentionally stubbed (404, "core API not implemented").

### CRUD

| Verb | Path | Behaviour |
|------|------|-----------|
| POST | collection | create; 201, or **409** if name exists. Sets `uid` (`uid-N`, sequential), `creationTimestamp` (RFC3339), `resourceVersion`. |
| GET | item | 200 with resource, or 404 |
| GET | collection | 200 with `{apiVersion, kind:"<resource>List", metadata, items}` |
| PUT | item | **upsert**: 200 always, creates if absent (never 404). Bumps `resourceVersion`; does **not** set uid/creationTimestamp. |
| DELETE | item | 200 with deleted resource, or 404 |
| PATCH | item | **simplified merge** (see below); 200 or 404 |

> Divergences from the old spec worth flagging: PUT is an upsert (no 404 on
> missing); `uid` is a sequential counter `uid-N`, not a UUID; uid/creationTimestamp
> are set on POST only. Error bodies are real k8s `Status` objects
> (`kind:"Status"`, `status:"Failure"`, `reason`, `code`).

### PATCH is not full merge-patch

`PATCH` ignores `Content-Type` and handles only three keys: `status` and `spec`
are **replaced wholesale** (not deep-merged); `metadata.labels` merge per-key
(values stringified). Other fields are silently dropped. Do not rely on it for
general strategic/merge-patch semantics.

## Fault injection

Faults are registered **per path-substring**, not globally:

```go
srv.SetFault("libvirtdomains", &fakek8s.FaultConfig{Code: 500, ErrorMsg: "boom"})
srv.SetFault("libvirtdomains", &fakek8s.FaultConfig{Delay: 100 * time.Millisecond})
srv.SetFault("libvirtdomains", nil)   // clear (pass nil, not FaultConfig{})
```

`FaultConfig{ Delay time.Duration; ErrorMsg string; Code int }`. A request whose
path *contains* the pattern gets the delay and/or error (`Code == 0` ⇒ no error).
Matching is `strings.Contains`. A separate global `SetDelay(d)` delays *every*
request.

> Field names are `Code`/`ErrorMsg` (not `Error`/`ErrorMessage`), and clearing is
> `SetFault(pattern, nil)` (not an empty struct).

There is also conflict injection — `InjectConflict(uuid, count)` /
`ClearConflicts()` — but note `hasConflict` is currently **not wired into any
handler** (dead code; flag before relying on it).

## Assertions & direct access

```go
srv.Requests() []RequestLog          // {Timestamp, Method, Path, Body}
srv.ClearRequests()
srv.GetResource(group, ver, res, ns, name) (*Resource, bool)
srv.SetResource(group, ver, res, ns, *Resource)   // bumps resourceVersion; no uid/ts
srv.ListResources(...) []*Resource;  srv.ListAllResources() []*Resource
srv.Reset()                          // clears resources+faults+requests, rv=0
```

`resource_updated`/`resource_deleted` events are emitted on PUT/DELETE (an
`EventCallback` hook exists) — **not** on POST create, PATCH, or `SetResource`.

## Web-UI wrapper (`fakek8s.APIServer`)

A separate REST layer (`api.go`) wraps the core server for the demo UI
([ADR 0028](../decisions/0028-tracing-and-timeline-ui.md)): `NewAPIServer(k8s)`,
routes under `/api/resources`, `/api/faults`, `/api/requests`, `/api/events`
(SSE), `/api/status`, `/api/reset`. Its DELETEs return 204 and its fault DTO uses
snake_case (`delay_ms`, `error_code`, `error_message`). Not needed for Go tests,
which use the core `Server` directly.
