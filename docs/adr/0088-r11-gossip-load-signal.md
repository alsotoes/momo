# 0088-r11-gossip-load-signal

## Status
Proposed

## Confidence
Low

## Context
R11 can move cold bytes off a full node, but it cannot avoid *sending work* to a
full node: no node knows how loaded its peers are. Membership gossip carries
alive/suspect/offline and round-trip time, not utilisation, so any redirect rule
would have to guess or fan out a query per request.

## Decision
- Fixed-size load payload: The system SHALL encode a load report in a fixed 20-byte payload containing disk utilisation percent, in-flight requests, replication queue depth, and a sample timestamp.
- Publication rides the heartbeat fanout: The system SHALL publish the local report to the same peers that receive a heartbeat, on the same cadence, and only when a load provider is installed.
- Peers store the latest report: The system SHALL store the most recent valid report on the peer record and expose it by peer ID.
- Peer load is exported as metrics: The system SHALL export the last report per peer as `momo_peer_disk_used_percent{peer}` and `momo_peer_inflight_requests{peer}` gauges, and SHALL declare the families even before any report arrives. ## Out of scope - Any redirect or admission decision (tracked in #1261). - Replication queue depth accounting (field reserved, reports zero today).

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/063-gossip-load-without-a-coordinator.md

## References
- Issue: #1261
- PR: 
- Spec: `openspec/changes/r11-gossip-load-signal/`
- Blog: docs/blog/posts/063-gossip-load-without-a-coordinator.md

