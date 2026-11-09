# Tasks — Eliminate Singletons + Hardcoded Constants

## Phase 1 — Eliminate Pheromone Router Singleton
- [x] `DefaultRouter` deleted; `NewPheromoneRouter()` is the only constructor.
- [x] The unused `DownloadWithFallback` wrapper is gone: production code already
  called `DownloadWithFallbackRouter`, so the singleton existed only to give the
  wrapper a default. The router is now a required argument.
- [x] A nil router returns `EINVAL` instead of silently falling back to shared
  state — a missing dependency is a programming error, not a behaviour switch.
- [x] Tests construct their own router; a dedicated test covers the nil case.
- [x] Caller lifetime is explicit: whoever downloads decides how long learned
  trails live, so two clients in one process no longer interfere.

## Phase 2 — Eliminate Global Payload Pool
- [x] The replication payload pool is now a `replicationPool` instance owned by
  the `ChangeReplicationModeServer` listener: its backing `sync.Pool`, adaptive
  target and size learner are per server, so two daemons in one process cannot
  resize or drain each other. The exported CLI helper keeps its own instance.
- [x] No package-level `payloadPool`/`payloadPoolTarget`/learner remain; the
  test-only swap is a method on the instance (`setBackingPool`).
- [x] Unit tests: adaptive growth/clamp/threshold, guarded release, and an
  isolation test proving one pool's resize leaves another at the floor.
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
- [x] `[server]` section: `handshake_timeout` (default 10 s),
  `metadata_deadline` (default 60 s), `max_concurrent_connections`
  (default 1000); all must be positive.
- [x] `[coordination] propagation_timeout` (default 11 s, must be positive).
- [x] Wire the deadlines through `src/server/deadlines.go`, which also supplies the
  historical default when the value is absent or zero (older configs and tests
  that build `common.Configuration` directly keep working).
- [x] `replication.go` acceptance semaphore uses
  `cfg.Server.MaxConcurrentConnections`; the client ACK wait keeps its own
  `clientAckTimeout` constant because it is a client-side guard, not a server
  propagation budget.
- [ ] `[replication] payload_pool_capacity` — the pool is already adaptive
  (`payloadPoolMinCapacity` .. 1 MiB, learned from observed sizes, #1145), so a
  fixed knob would be a downgrade. Deferred to the per-node pool work (Phase 2).
- [ ] `src/client/pheromone.go` evaporation interval — belongs with the Phase 1
  router injection (the package-level `DefaultRouter` owns that constant).
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