## ADDED Requirements
### Requirement: Primary Splay QUIC Protocol
The system SHALL utilize the QUIC protocol (UDP) exclusively when executing a `ReplicationPrimarySplay` replication mode (ID 3) to improve stability and throughput over Wide Area Networks (WAN).

#### Scenario: Client Primary Splay upload
- **WHEN** the cluster is operating under `ReplicationPrimarySplay` mode
- **THEN** the client must establish a QUIC connection with Daemon 0, Daemon 1, and Daemon 2 concurrently
- **AND** the payload and metadata must be streamed utilizing QUIC multiplexing instead of TCP

### Requirement: Protocol Hot-Swapping
The daemons SHALL support seamless transitioning between TCP and QUIC listeners based on the dynamic replication mode broadcast.

#### Scenario: Switching from TCP to QUIC
- **WHEN** the polymorphic controller shifts the replication scheme from `ReplicationSplay` (TCP) to `ReplicationPrimarySplay` (QUIC)
- **THEN** new client connections must be accepted and routed on the UDP/QUIC listener
- **AND** existing TCP connections from the old configuration must complete their file chunk downloads cleanly
- **AND** no daemons are restarted to apply the new configuration
