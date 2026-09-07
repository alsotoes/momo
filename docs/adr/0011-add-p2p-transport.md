# 0011-add-p2p-transport

## Status
Proposed

## Confidence
Low

## Context
Momo's current architecture relies on a static configuration (`momo.conf`) to define the network topology. The logic for replication (especially in modes like Chain and Splay) involves direct, hardcoded connections between servers. Furthermore, the role of Server ID 0 as the central authority for metrics and mode changes creates a single point of failure (SPOF) and a potential performance bottleneck. If server 0 goes down, the system's dynamic capabilities are lost.

## Decision
- P2P Gossip Node Discovery & Joining (Resolves #153): The system SHALL support elastic membership where new storage nodes can dynamically join the cluster by introducing themselves to a known bootstrap peer. The bootstrap peer and joining node SHALL exchange and disseminate membership information across the entire network via regional gossip heartbeats.
- Gossip Liveness Detection & Heartbeats (Resolves #153): The system SHALL continuously detect node failures, network partitions, or silent dropouts using background gossip heartbeats.
- Graceful Node Departure (Resolves #153): The system SHALL support graceful node decommissioning, ensuring that files stored on the departing node are safely re-balanced to other replica nodes before exit.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/016-p2p-gossip-swim.md

## References
- Issue: #153
- PR: #808
- Spec: `openspec/changes/add-p2p-transport/`
- Blog: docs/blog/posts/016-p2p-gossip-swim.md

