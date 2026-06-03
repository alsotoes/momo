## ADDED Requirements
### Requirement: Decoupled Communication Architecture (Issue #131)
The system SHALL strictly separate the communication protocol (e.g., `momo-quic`, `s3-tcp`) from the core replication logic (`None`, `Chain`, `Splay`, `PrimarySplay`).

#### Scenario: Switching protocols without affecting replication
- **GIVEN** a cluster operating in `Chain` replication mode
- **WHEN** the `protocol` is changed from `momo-tcp` to `momo-quic`
- **THEN** the path of data (Node 0 -> Node 1 -> Node 2) must remain identical
- **AND** the sequence of handshake, metadata, and payload steps must be handled by the protocol layer, while the "forward to next node" logic is handled by the core.

### Requirement: Unified 'Communicator' Abstraction
The system SHALL implement a `Communicator` interface that encapsulates transport-specific connection management and protocol-specific handshaking, exposing a clean stream for the core logic.

#### Scenario: Transport-Agnostic Replication Execution
- **WHEN** the core replication logic needs to send a file to a peer
- **THEN** it must request a `Communicator` from the `ProtocolFactory`
- **AND** use the interface's `SendFile` method which handles the specific auth and mode-negotiation steps required by that protocol (e.g., S3 headers vs Momo 83-byte packet).
- **AND** receive a success/failure status consistent across all protocol implementations.

### Requirement: Robust Stack Configuration
The system SHALL utilize a single composite `protocol` string in the `[global]` section of `momo.conf` to configure the network stack.

#### Scenario: Validating stack configuration
- **GIVEN** a configuration with an unknown protocol string (e.g., `protocol=xyz-ftp`)
- **WHEN** the system initializes
- **THEN** it must log a CRITICAL error and fail to start
- **GIVEN** a missing protocol field
- **WHEN** the system initializes
- **THEN** it must log a WARNING: "No protocol definition found, falling back to default (momo-tcp)"
- **AND** proceed using the legacy TCP stack

### Requirement: Universal QUIC/TCP Coexistence
The daemons SHALL support simultaneous listening for both TCP and QUIC packets on the same configured host address, allowing the cluster to handle heterogeneous clients.

#### Scenario: Transport-Agnostic File Processing
- **WHEN** a file is received via QUIC/S3 or TCP/Momo
- **THEN** the core business logic (replication fan-out, storage, hash validation) must remain identical and unaffected by the transport choice
