> GitHub Issue URL: https://github.com/alsotoes/momo/issues/153

# P2P Transport & Elastic Membership Specification

## Purpose
This specification defines the communication wire flows, bootstrap processes, and liveness detection protocols for Peer-to-Peer (P2P) transport and elastic membership in Momo. This eliminates static server cluster configurations and enables a fully decentralized, self-discovering peer ring.

## ADDED Requirements

### Requirement: P2P Gossip Node Discovery & Joining (Resolves #153)
The system SHALL support elastic membership where new storage nodes can dynamically join the cluster by introducing themselves to a known bootstrap peer. The bootstrap peer and joining node SHALL exchange and disseminate membership information across the entire network via regional gossip heartbeats.

#### Scenario: New node joining via bootstrap peer
- **GIVEN** a newly spawned node is started with the address of a valid bootstrap peer
- **WHEN** the node initiates a connection to the bootstrap peer
- **THEN** the peer validates its authentication token, records the joining node's ID, weight, and regional attributes, updates its cluster map, and disseminates this new membership status asynchronously to $k$ random peers over lightweight gossip UDP/QUIC handshakes

### Requirement: Gossip Liveness Detection & Heartbeats (Resolves #153)
The system SHALL continuously detect node failures, network partitions, or silent dropouts using background gossip heartbeats.

#### Scenario: Node failure and cluster convergence
- **GIVEN** a node crashes or becomes unreachable over the network
- **WHEN** sibling nodes fail to receive a gossip heartbeat from the failed node within a configured timeout window (e.g., 5 seconds)
- **THEN** the neighboring nodes mark the failed node as "suspect" and gossip this status. If no heartbeat is received within the suspicion window, the failed node is permanently marked as "offline" across all active cluster maps, prompting CRUSH placement calculations to automatically route traffic to healthy replica nodes

### Requirement: Graceful Node Departure (Resolves #153)
The system SHALL support graceful node decommissioning, ensuring that files stored on the departing node are safely re-balanced to other replica nodes before exit.

#### Scenario: Gracefully leaving the cluster map
- **GIVEN** a node is triggered to decommission or leave the cluster
- **WHEN** the decommission command is received
- **THEN** the leaving node broadcasts a "graceful departure" packet, coordinates with remaining peers to hand over its stored CAS file mappings, waits for replication confirmations, and safely terminates its daemon sockets
