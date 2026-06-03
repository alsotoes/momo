## ADDED Requirements
### Requirement: Universal QUIC Transport Support
The system SHALL support the QUIC protocol (UDP) as a standard transport layer for ALL replication modes (None, Chain, Splay, PrimarySplay). This enables higher resilience and mandatory encryption for both external client uploads and internal cluster replication.

#### Scenario: Global QUIC utilization
- **WHEN** the cluster is configured to prefer QUIC
- **THEN** all data transfers (External and Internal) must establish QUIC connections
- **AND** the payload and metadata must be streamed utilizing QUIC multiplexing
- **AND** all traffic must be encrypted via TLS 1.3 as mandated by the protocol

### Requirement: Transport-Agnostic Processing
The daemons SHALL implement a transport-agnostic processing layer that handles metadata and file streams identically, regardless of whether the underlying transport is TCP or QUIC.

#### Scenario: Seamless Protocol Coexistence
- **WHEN** a daemon receives concurrent file transfers on both its TCP and UDP/QUIC listeners
- **THEN** it must process both streams using the same business logic, validation rules (SHA-256), and storage paths
- **AND** it must maintain separate connection quotas for each transport to prevent resource exhaustion

### Requirement: Protocol Configuration
The system SHALL expose a `protocol` setting in the `[global]` section of the configuration file to allow explicit selection of the transport layer.

#### Scenario: Configuring the transport protocol
- **GIVEN** a configuration file with `[global]` section containing `protocol=quic`
- **WHEN** the daemon starts or a client initiates a transfer
- **THEN** the system must utilize the QUIC transport for all replication modes
- **AND** if `protocol=tcp` is set, the system must maintain its original TCP-only behavior
- **AND** the default protocol SHALL be `tcp` if the field is missing

### Requirement: Protocol Hot-Swapping
The system SHALL support seamless transitioning between TCP and QUIC based on dynamic configuration or network conditions.

#### Scenario: Switching preferred transport
- **WHEN** the cluster-wide configuration shifts the preferred transport from TCP to QUIC
- **THEN** new connections must attempt to dial via QUIC first, falling back to TCP only if specified
- **AND** existing connections must complete their transfers using their original transport cleanly
- **AND** the transition must not require manual daemon restarts or cause data corruption
