# 0012-add-peer-quality-quorum-selection

## Status
Accepted

## Confidence
High

## Context


## Decision
- Per-Peer RTT Tracking: Each `Peer` SHALL expose an EWMA round-trip time via `SetRTT(dur)` and `RTT()` accessors. The gossiper SHALL write its ping-derived EWMA sample to the target peer so `Peer.RTT()` reflects fresh liveness data.
- Quality-Aware Alive Selection: `PeerMap` SHALL provide `AliveByQuality()` returning the alive peers (excluding `Suspect` and `Offline`) sorted by RTT ascending (best first). Peers with unknown RTT (0) SHALL sort after known-RTT peers but remain included while alive.
- Quorum Consumers Use Quality Ordering: The scatter-gather query, lease acquire, and OPRF evaluation paths SHALL select their quorum from `AliveByQuality()` so the lowest-RTT alive peers are preferred.
- Wire Stability: This change SHALL NOT add, remove, or reinterpret any wire message type, payload field, or byte layout (Rules 7, 38). Peer quality is a local in-memory ranking.
- Concurrency Safety: `AliveByQuality` and `Peer` RTT accessors SHALL be safe for concurrent use.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-peer-quality-quorum-selection/
- Blog: docs/blog/posts/...md
