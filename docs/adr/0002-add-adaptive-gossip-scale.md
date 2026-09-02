# 0002-add-adaptive-gossip-scale

## Status
Accepted

## Confidence
High

## Context


## Decision
- Adaptive Fanout Computation: The gossip layer SHALL compute the default (adaptive) fanout for a given alive peer count `N` as `clamp(ceil(ln N), minFanout, maxFanout)` with `minFanout = 1` and `maxFanout = 10` (Rule 32).
- Config Semantics: `fanout = 0` (or unset) SHALL mean adaptive; `fanout > 0` SHALL be an explicit fixed override.
- Resolution at Send Time: Fanout SHALL be resolved per heartbeat from the current alive peer count, so it tracks cluster membership changes without a restart.
- Wire Stability: This change SHALL NOT alter heartbeat/membership RPC message types, payloads, or byte layouts (Rules 7, 38).
- Concurrency Safety: Fanout resolution SHALL read the peer map safely under concurrency.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-adaptive-gossip-scale/
- Blog: docs/blog/posts/...md
