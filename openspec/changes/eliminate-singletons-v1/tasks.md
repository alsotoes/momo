# Tasks — Eliminate Singletons + Hardcoded Constants

## Phase 1 — Eliminate Pheromone Router Singleton
- [ ] Modify `src/client/client.go`:
  - Add `PheromoneRouter` parameter to `Connect`, `DownloadWithFallback`, `DownloadWithFallbackRouter`
  - Remove `DefaultRouter` reference; callers pass router
  - Update all call sites in `momo.go` and tests
- [ ] Delete `DefaultRouter` from `src/client/pheromone.go:70`
- [ ] Update `src/client/pheromone.go`:
  - `NewPheromoneRouter()` remains public for construction
  - Remove package-level `var DefaultRouter`
- [ ] Update tests: inject mock `PheromoneRouter` where needed
- [ ] Unit tests: verify mock router works in client tests

## Phase 2 — Eliminate Global Payload Pool
- [ ] Modify `src/server/replication.go`:
  - Remove package-level `payloadPool` variable
  - Add `payloadPool` field to `Daemon` struct (or new `ReplicationContext` struct)
  - `payloadPoolCapacity` from config (default 1024)
  - `releasePayload` becomes method on `Daemon`/`ReplicationContext`
- [ ] Add `[replication] payload_pool_capacity` config parsing in `config.go`
- [ ] Update `ChangeReplicationModeServer` / `ChangeReplicationModeClient` to use Daemon's pool
- [ ] Unit tests: verify pool isolation between nodes

## Phase 3 — Encapsulate Replication State
- [ ] Create `src/server/state.go` (or add to `server.go`):
  - Move `replicationStateMutex`, `currentReplicationMode`, `replicationState` to `Daemon` struct
  - `GetReplicationState()` / `SetReplicationState()` / `GetCurrentReplicationMode()` as methods
- [ ] Update `src/server/replication.go`:
  - Remove package-level globals
  - Access state via `Daemon` methods
- [ ] Update `src/server/server.go`:
  - `Daemon` passes its state methods to `ChangeReplicationModeServer`
- [ ] Update `src/server/replication.go:243` (propagation check):
  - Replace `if 0 == serverId` with coordinator role check (from #1144)
- [ ] Unit tests: verify state isolation, concurrent access safety

## Phase 4 — Externalize Hardcoded Constants
- [ ] Config parsing additions in `src/common/config.go`:
  - `[replication] payload_pool_capacity` (default 1024)
  - `[server] max_concurrent_connections` (default 1000)
  - `[server] handshake_timeout` (default 10s)
  - `[server] metadata_deadline` (default 60s)
  - `[coordination] propagation_timeout` (default 11s)
- [ ] Replace hardcoded values with config lookups:
  - `src/server/replication.go:21` → `cfg.Replication.PayloadPoolCapacity`
  - `src/server/replication.go:143` → `cfg.Server.MaxConcurrentConnections`
  - `src/client/pheromone.go:27` → adaptive or config
  - `src/server/server.go:335` → `cfg.Server.HandshakeTimeout`
  - `src/server/server.go:431` → `cfg.Server.MetadataDeadline`
  - `src/server/replication.go:271` → `cfg.Coordination.PropagationTimeout`
- [ ] Update `src/common/struct.go` with new config structs
- [ ] Integration tests: verify config values used

## Phase 5 — Dependency Injection Wiring
- [ ] Modify `src/momo.go`:
  - Construct all dependencies before `Daemon` creation
  - `PheromoneRouter` → passed to client functions
  - `PayloadPool` → created on `Daemon`
  - `Coordinator` (from #1144) → passed to `GetMetrics`, `ChangeReplicationModeServer`
  - `AdaptiveLearner` (from #1145) → passed to components needing constants
- [ ] Update `src/server/server.go` `Daemon` signature to accept dependencies
- [ ] Update `src/client/client.go` to accept `PheromoneRouter` parameter
- [ ] Verify all call sites updated

## Phase 6 — Test Infrastructure + Adaptive Integration
- [ ] Test updates:
  - All tests inject mock `PheromoneRouter`
  - Tests construct `Daemon` with mock dependencies
  - No tests rely on global state
- [ ] Adaptive integration (see #1145):
  - Components check `adaptive.Enabled` before using config
  - If `adaptive.enabled=true` and learner has value → use learned
  - Else → use config value
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with new config keys
- [ ] Update `docs/ARCHITECTURE.md` with dependency injection section

## Phase 7 — Validation
- [ ] `go build ./...` clean
- [ ] `go test ./...` passes (including parallel tests)
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Chaos test: parallel test runs → no global state interference
- [ ] Benchstat: no regression on hot paths
- [ ] Blog post per Rule 76 (teach the DI wiring pattern)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [ ] PR body with `Resolves #1149`

## Definition of Done (Per Phase)
- All phase tasks checked
- All tests pass (including parallel)
- No global mutable state accessed in hot paths
- All constants configurable or adaptive
- Reviewer ✅ gate (Rule 55)