# 0012-add-peer-quality-quorum-selection

## Status
Accepted

## Confidence
High

## Context
Momo's decentralized P2P layer runs quorum-based operations over the gossip
transport:

- **Scatter-Gather** queries (`src/p2p/scatter_gather.go`) broadcast to every
  alive peer,
- **Lease** consensus (`src/p2p/lease.go`) requests a majority from alive peers,
  and
- **Threshold-OPRF** evaluation (`src/p2p/oprf_rpc.go`) asks a quorum of alive
  peers for key shares.

Each of these currently selects its quorum with `Peers().Alive()` — which simply
returns **all** peers in the `PeerStateAlive` state with **no regard for peer
quality**. The gossip layer already tracks per-peer round-trip time (EWMA) in
`rttTracker` and liveness state, but quorum selection ignores it. Consequences:

- a slow, high-latency, or region-remote peer can be selected for a lease or
  OPRF quorum even when a fast, well-connected peer is available;
- scatter-gather wastes time contacting high-RTT/stale peers that are least
  likely to respond within the timeout window;
- this is inconsistent with the region-aware routing intent of Rule 41 (prefer
  low-cost members).

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
- **Blog post**: docs/blog/posts/018-adaptive-scaling-peer-quality.md

## References
- Issue: #823
- PR: #833
- Spec: `openspec/changes/add-peer-quality-quorum-selection/`
- Blog: docs/blog/posts/018-adaptive-scaling-peer-quality.md

