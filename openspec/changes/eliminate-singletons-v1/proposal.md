# Change: Eliminate Singletons + Hardcoded Constants — Per-Node Injected Dependencies

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1149 (P2: Eliminate Singletons + Hardcoded Constants)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current Momo has **singletons and package-level globals** that violate:
- **Diversity is Strength** (ADAPTIVE_SYSTEMS.md): Monoculture is fragile
- **Stigmergy**: Local rules need local state, not shared globals
- **Zero SPOF**: Global state = implicit coordination
- **Rule 74 (Seam-Over-Plugins)**: Compile-time seams, not runtime globals

| Location | Global | Problem |
|----------|--------|---------|
| `src/client/pheromone.go:70` | `DefaultRouter = NewPheromoneRouter()` | Package singleton; tests can't inject mock |
| `src/server/replication.go:32` | `var payloadPool = &sync.Pool{...}` | Global pool; all nodes share sizing |
| `src/server/replication.go:38-44` | `replicationStateMutex`, `currentReplicationMode`, `replicationState` | Global replication state |
| `src/metrics/metrics.go:138` | `serverId != 0` early return | Hardcoded coordinator role |
| Multiple files | Hardcoded constants (timeouts, capacities, thresholds) | Not adaptive, not testable |

## What Changes

### Phase 1: Eliminate Pheromone Router Singleton
- Remove `DefaultRouter` package variable
- `PheromoneRouter` injected via constructor in `client.Connect` / `client.DownloadWithFallback`
- Tests inject mock router; production uses `NewPheromoneRouter()`

### Phase 2: Eliminate Global Payload Pool
- Replace `payloadPool` global with per-node pool in `server.Daemon` struct
- `payloadPoolCapacity` becomes configurable (or adaptive via #1145)
- `releasePayload` becomes method on Daemon

### Phase 3: Encapsulate Replication State
- Move `replicationStateMutex`, `currentReplicationMode`, `replicationState` into `server.Daemon` or `coordination.Coordinator`
- `GetReplicationState()` / `SetReplicationState()` become methods
- `ChangeReplicationModeServer` accesses via Daemon reference

### Phase 4: Hardcoded Constants → Config/Adaptive
| Constant | Current | New Location |
|----------|---------|--------------|
| `payloadPoolCapacity` (1024) | `replication.go:21` | `[replication] payload_pool_capacity` or adaptive |
| `maxConcurrentConnections` (1000) | `replication.go:143` | `[server] max_concurrent_connections` or adaptive |
| `EvaporationInterval` (1s) | `pheromone.go:27` | `[adaptive] evaporation_interval` or adaptive |
| Handshake timeout (10s) | `server.go:335` | `[server] handshake_timeout` or adaptive |
| Metadata deadline (60s) | `server.go:431` | `[server] metadata_deadline` or adaptive |
| Propagation timeout (11s) | `replication.go:271` | `[coordination] propagation_timeout` or adaptive |

### Phase 5: Dependency Injection Wiring
- `momo.go` / `Daemon` constructs all dependencies
- Dependencies passed explicitly (not globals)
- Tests can inject mocks/stubs

## Configuration

```toml
[replication]
payload_pool_capacity = 1024          # or 0 = adaptive (see #1145)

[server]
max_concurrent_connections = 1000     # or 0 = adaptive
handshake_timeout = "10s"
metadata_deadline = "60s"

[coordination]
propagation_timeout = "11s"
```

## Non-Goals

- Full DI framework (manual wiring sufficient)
- Runtime hot-swap of dependencies (compile-time seams per Rule 74)
- Per-request dependency scoping (node-scoped is sufficient)

## Impact

- **Testability**: All components injectable, mockable
- **Principles**: Diversity (no monoculture), Stigmergy (local state), Zero SPOF (no globals)
- **Operations**: All constants configurable or adaptive
- **Rule 74**: Compile-time seams, not runtime globals