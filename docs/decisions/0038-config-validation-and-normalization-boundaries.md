<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# 0038 — Define boundaries for config validation and normalization

Status: open · Date: 2026-09 · Depends on: 0034, 0036

## Context

The controller-runtime branch added more derived wiring between user config and
runtime components. One concrete example is heartbeat TTL: consumers need a TTL
large enough to cover heartbeat throttling and a muted poll, so `operator` derives
an exporter `HeartbeatTTL` from `PollInterval` and `HeartbeatPeriod`.

That raised a placement question: should guards, defaults, and sanitizing happen
centrally in `internal/config`, or close to the logic that understands why a value
needs guarding? The current config package already provides `DefaultConfig`, YAML
loading, env overrides, flag binding, and `Validate`. It mostly rejects invalid
input or supplies defaults before overrides; it does not yet establish a broad
"normalize after parse" step that rewrites values for downstream packages.

The tension is real. Central validation makes startup behaviour easier to reason
about and prevents downstream packages from each inventing their own defaults.
But central normalization can also scatter requirements: the reason a value must
be transformed may live in edge logic such as heartbeat staleness semantics,
controller-runtime worker setup, or retention behaviour.

## Decision

No broad config-normalization layer is introduced yet.

For now, keep exporter and controller constructors simple: they should receive
values that are already valid for their purpose and should not silently repair
operator-level config. Derived values may remain near the operator wiring when the
derivation is about connecting components rather than parsing user input.

The open question is whether `internal/config` should grow explicit derived-value
methods or a normalization step if more cases appear. A small method such as
`Config.HeartbeatTTL()` may be preferable to a general `Normalize()` if the need
remains isolated.

## Consequences

The current code avoids introducing a new lifecycle phase for config just to serve
one derived exporter value. This keeps the change small and avoids presenting a
partial normalization layer as an established project convention.

The cost is that some derived runtime values are still computed outside
`internal/config`. Future contributors may disagree about whether a guard belongs
near the consumer or in config validation. If more such cases accumulate, this ADR
should be revisited and replaced by a concrete convention.

## Alternatives rejected

- **Add `Config.Normalize()` now.** Too broad for one known case, and it would
  create a new config lifecycle phase whose ordering with defaults, YAML, env, and
  flags must be maintained.
- **Let downstream packages silently default invalid values.** This hides config
  errors and makes startup behaviour depend on which component sees a value first.
- **Move every derived value into `internal/config` immediately.** This centralizes
  outputs but can also move component-specific reasoning away from the code that
  explains why the derivation exists.
- **Reject all non-positive optional tuning values.** Some fields intentionally use
  zero as a meaningful value, for example no rate limit or heartbeat every poll;
  validation must preserve those semantics.
