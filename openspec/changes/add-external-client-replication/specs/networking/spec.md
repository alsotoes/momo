## ADDED Requirements

### Requirement: External S3 Client Detection
The system SHALL detect external S3 clients by the absence of the
`X-Momo-Requested-Mode` header and treat them as direct client connections
(equivalent to `DummyEpoch` timestamp).

#### Scenario: aws-cli PUT to server in primary-splay mode
- **GIVEN** a server with `replication_order=3,2,1` and `client_side_replication_modes=3`
- **AND** the server's current replication mode is `3` (primary-splay)
- **WHEN** an external S3 client sends a PUT without `X-Momo-Requested-Mode`
- **THEN** the server must detect this as an external client connection
- **AND** downgrade to the next server-side mode in `replication_order` (mode 2, splay)
- **AND** replicate the file using server-side splay replication
- **AND** the global polymorphic state must remain unchanged at mode 3

#### Scenario: momo CLI client unaffected by downgrade
- **GIVEN** the same server configuration
- **WHEN** a momo CLI client connects with `DummyEpoch` timestamp
- **THEN** the server must use mode 3 (primary-splay) as normal
- **AND** the client performs fan-out replication

### Requirement: Configurable Client-Side Replication Modes
The system SHALL support a `client_side_replication_modes` config variable
containing a comma-separated list of replication mode IDs that require a
momo-aware client. Any mode in this list is automatically skipped for external
S3 clients via set subtraction:
`effective_modes = replication_order \ client_side_replication_modes`

#### Scenario: Future mode added to client_side_replication_modes
- **GIVEN** `client_side_replication_modes=3,4` where mode 4 is a future mixed mode
- **WHEN** an external S3 client connects and the server's mode is 4
- **THEN** the server must skip mode 4 and use the next server-side mode
- **AND** no code changes are required beyond the config update

#### Scenario: Default when config is unset
- **GIVEN** `client_side_replication_modes` is not present in config
- **THEN** the system must default to `3` (primary-splay)

### Requirement: Per-Transaction Downgrade Without Global Mutation
The per-connection mode downgrade MUST NOT mutate the global polymorphic
replication state. The downgrade applies to the current transaction only.

#### Scenario: Concurrent connections with different clients
- **GIVEN** global state is mode 3 (primary-splay)
- **WHEN** a momo CLI client (T0) and an aws-cli client (T1) connect concurrently
- **THEN** T0 must use mode 3 (primary-splay)
- **AND** T1 must use mode 2 (splay) for its transaction only
- **AND** the global state must remain 3 after both transactions complete
