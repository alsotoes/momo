# Eliminate Singletons + Hardcoded Constants — Per-Node Injected Dependencies

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1149

## Purpose

Eliminate all **package-level singletons** and **global mutable state**, replacing them with **per-node injected dependencies** via compile-time seams (Rule 74). All hardcoded constants become configurable or adaptive.

## Requirements

### Requirement 1: Eliminate Pheromone Router Singleton

#### Scenario: Pheromone router injected, not global
**Given** `client.Connect` / `client.DownloadWithFallback` / `client.DownloadWithFallbackRouter`
**When** functions are called
**Then** `PheromoneRouter` is passed as parameter (or via config struct)
**And** `DefaultRouter` package variable removed
**And** `NewPheromoneRouter()` called at construction site (e.g., `momo.go`)

#### Scenario: Test injects mock router
**Given** a test calls client functions
**When** test provides mock `PheromoneRouter`
**Then** test controls replica selection behavior
**And** no global state pollution between tests

### Requirement 2: Eliminate Global Payload Pool

#### Scenario: Payload pool per-node, not global
**Given** `server.Daemon` starts
**When** payload pool needed
**Then** pool created as field on `Daemon` struct
**And** `payloadPoolCapacity` from config (or adaptive)
**And** `releasePayload` becomes method on `Daemon`

#### Scenario: Pool capacity configurable
**Given** `[replication] payload_pool_capacity` in config
**When** daemon initializes pool
**Then** capacity = config value (or adaptive via #1145)
**And** default = 1024 (preserves current behavior)

### Requirement 3: Encapsulate Replication State

#### Scenario: Replication state encapsulated, not global
**Given** `server.Daemon` or `coordination.Coordinator` owns replication state
**When** replication mode accessed/modified
**Then** `GetReplicationState()` / `SetReplicationState()` are methods on owner
**And** package-level `replicationStateMutex`, `currentReplicationMode`, `replicationState` removed
**And** `ChangeReplicationModeServer` accesses via Daemon reference

### Requirement 4: Hardcoded Constants → Config/Adaptive

#### Scenario: All hardcoded constants externalized
**Given** daemon starts
**When** constants needed
**Then** values sourced from config (with safe defaults) or adaptive learner:

| Constant | Source |
|----------|--------|
| `payloadPoolCapacity` | `[replication] payload_pool_capacity` (default 1024) |
| `maxConcurrentConnections` | `[server] max_concurrent_connections` (default 1000) |
| `EvaporationInterval` | `[adaptive] evaporation_interval` or adaptive |
| Handshake timeout | `[server] handshake_timeout` (default 10s) |
| Metadata deadline | `[server] metadata_deadline` (default 60s) |
| Propagation timeout | `[coordination] propagation_timeout` (default 11s) |

#### Scenario: Adaptive integration
**Given** `adaptive.enabled=true` (see #1145)
**When** constant needed
**Then** value from adaptive learner (overrides config)
**And** config value used as initial hint

### Requirement 5: Dependency Injection Wiring

#### Scenario: All dependencies wired at construction
**Given** `momo.go` starts daemon
**When** `Daemon` created
**Then** all dependencies constructed and passed:
- `PheromoneRouter` (for client downloads)
- `PayloadPool` (for replication)
- `Coordinator` (for distributed coordination)
- `AdaptiveLearner` (for thresholds/constants)
- `LeaseManager`, `Gossiper`, `Storage`, etc.

#### Scenario: Tests inject mocks
**Given** a unit test
**When** test constructs component
**Then** test provides mock implementations for all interfaces
**And** no global state accessed
**And** tests run in parallel without interference

### Requirement 5: Configuration Schema

```toml
[replication]
payload_pool_capacity = 1024          # 0 = adaptive (see #1145)

[server]
max_concurrent_connections = 1000     # 0 = adaptive
handshake_timeout = "10s"
metadata_deadline = "60s"

[coordination]
propagation_timeout = "11s"
```

### Requirement 6: Backward Compatibility

#### Scenario: Config defaults preserve current behavior
**Given** config keys not specified
**When** daemon starts
**Then** all defaults match current hardcoded values
**And** zero behavioral change for existing deployments
**And** adaptive features opt-in via `adaptive.enabled=true`

## Configuration Schema

```go
type ConfigurationEliminateSingletons struct {
	// Replication
	PayloadPoolCapacity int `ini:"payload_pool_capacity"`

	// Server
	MaxConcurrentConnections int           `ini:"max_concurrent_connections"`
	HandshakeTimeout         time.Duration `ini:"handshake_timeout"`
	MetadataDeadline         time.Duration `ini:"metadata_deadline"`

	// Coordination
	PropagationTimeout time.Duration `ini:"propagation_timeout"`
}
```

## Acceptance Criteria

- [ ] `DefaultRouter` removed; all callers inject `PheromoneRouter`
- [ ] `payloadPool` removed; per-node pool on `Daemon`
- [ ] Global replication state removed; encapsulated in `Daemon`/`Coordinator`
- [ ] All hardcoded constants → config keys with safe defaults
- [ ] Adaptive integration: config values used as hints when `adaptive.enabled=true`
- [ ] All components injectable; tests use mocks without global state
- [ ] `go build ./...` clean; `go test ./...` passes
- [ ] Zero behavioral change with default config

## Failure Scenarios

### Config Missing
```
1. Config key not specified
2. Default value used (matches old hardcoded value)
3. Logs debug: "using default payload_pool_capacity=1024"
```

### Adaptive Override
```
1. adaptive.enabled=true
2. Learned value available
3. Learned value used (config ignored)
4. Logs debug: "adaptive payload_pool_capacity=4096 (config=1024)"
```

## Out of Scope

- Full DI framework (manual wiring sufficient)
- Runtime hot-swap of dependencies (compile-time seams per Rule 74)
- Per-request dependency scoping (node-scoped sufficient)
- Eliminating all package-level constants (only mutable globals)