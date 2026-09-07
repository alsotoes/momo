# 0002-add-adaptive-gossip-scale

## Status
Accepted

## Confidence
High

## Context
Momo's gossip layer (`src/p2p/gossip.go`) uses a **fixed** fanout
(`DefaultGossipConfig.Fanout = 3`, overridable via `[p2p] fanout`). The number
of peers a node gossips to per heartbeat does not scale with cluster size:

- On a **small** cluster (e.g. 2 nodes), a fanout of 3 wastes the extra two
  sends (there are only 1-2 eligible peers) and adds redundant RTT probes.
- On a **large** cluster (e.g. 50 nodes), a fanout of 3 provides slow gossip
  convergence — information (membership joins/leaves, tombstones) can take many
  rounds to propagate.

Gossip literature recommends a fanout proportional to `ln N` to bound message
amplification while keeping convergence time bounded.

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
- **Blog post**: docs/blog/posts/018-adaptive-scaling-peer-quality.md

## References
- Issue: #825
- PR: #833
- Spec: `openspec/changes/add-adaptive-gossip-scale/`
- Blog: docs/blog/posts/018-adaptive-scaling-peer-quality.md

