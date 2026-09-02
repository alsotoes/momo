# 0011-add-p2p-transport

## Status
Accepted

## Confidence
High

## Context


## Decision
- P2P Gossip Node Discovery & Joining (Resolves #153): The system SHALL support elastic membership where new storage nodes can dynamically join the cluster by introducing themselves to a known bootstrap peer. The bootstrap peer and joining node SHALL exchange and disseminate membership information across the entire network via regional gossip heartbeats.
- Gossip Liveness Detection & Heartbeats (Resolves #153): The system SHALL continuously detect node failures, network partitions, or silent dropouts using background gossip heartbeats.
- Graceful Node Departure (Resolves #153): The system SHALL support graceful node decommissioning, ensuring that files stored on the departing node are safely re-balanced to other replica nodes before exit.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-p2p-transport/
- Blog: docs/blog/posts/...md
