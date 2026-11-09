# Learned State Observability

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1287

### Requirement: Detailed health reports learned state

`GET /health/detailed` SHALL include a `learned_state` object describing what the
node learned at runtime and whether it is persisted.

#### Scenario: Controller and persisted mode

- **Given** a node with `[coordination] enabled=true` that holds the lease
- **And** the persisted replication mode is 2 with order `[1,2,3,4]`
- **When** an operator requests `/health/detailed`
- **Then** `learned_state.controller.is_holder` is `true`, `election_enabled` is
  `true`, and `lease_ttl_seconds` matches the configured TTL
- **And** `learned_state.replication_mode` reports mode 2, index 1, the order,
  `persisted: true`, and the relative store name `coordination/replication_mode`

#### Scenario: No provider installed

- **Given** a collector constructed without a learned-state provider
- **When** an operator requests `/health/detailed`
- **Then** the response omits `learned_state` entirely
- **And** all pre-existing fields are unchanged

### Requirement: The payload never leaks infrastructure paths

`learned_state` SHALL report persistence as a boolean plus a relative store name.

#### Scenario: No absolute paths

- **Given** any configuration with a data directory
- **When** `/health/detailed` is rendered
- **Then** no field value contains an absolute filesystem path
