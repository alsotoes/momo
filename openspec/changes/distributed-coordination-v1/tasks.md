# Tasks — Distributed Coordination (Metrics Controller + Replication Propagation)

## Phase 1 — Core Infrastructure: Lease Election for Coordination Roles
- [ ] Create `src/coordination/roles.go` — coordination role definitions
  - `RoleType` enum: `MetricsController`, `ReplicationPropagator`
  - `RoleConfig` struct with lease TTL, election retry interval
  - `RoleState` struct for persisted state (currentIndex, start time, etc.)
- [ ] Create `src/coordination/election.go` — lease-based role election
  - `Coordinator` struct wrapping `LeaseManager` + `Gossiper`
  - `Campaign(role RoleType)` — acquire lease, register with gossip
  - `Resign(role RoleType)` — release lease, persist state
  - `GetLeaseHolder(role RoleType) (nodeID int32, ok bool)` — query via gossip
- [ ] Create `src/coordination/state.go` — state persistence to BoltDB
  - `PersistState(role RoleType, state RoleState)` — write to `replication_state` bucket
  - `LoadState(role RoleType) (RoleState, error)` — read from bucket
  - `StateKey(role RoleType) string` — bucket key per role
- [ ] Add `[coordination]` section parsing in `config.go`
  - `ConfigurationCoordination` struct with all configurable fields
  - Default values matching spec
  - Validation: TTL > 0, intervals > 0
- [ ] Wire coordination config in `GetConfig` (config.go)
- [ ] Unit tests: `coordination_election_test.go` (mock LeaseManager, verify campaign/resign)

## Phase 2 — Distributed Metrics Controller
- [ ] Modify `src/metrics/metrics.go`:
  - Add `Coordinator` dependency to `GetMetrics`
  - Replace `if serverId != 0 { return }` with lease holder check
  - `GetMetrics` now runs on ALL nodes but only lease holder executes logic
  - Non-lease-holders: return early, log "not metrics controller lease holder"
- [ ] Add state persistence in metrics loop:
  - On `currentIndex` change: persist via `Coordinator.PersistState`
  - On loop start: load state via `Coordinator.LoadState`
- [ ] Add lease holder monitoring:
  - If lease lost while running: stop loop, persist state, return
  - If lease acquired while running: load state, resume loop
- [ ] Update `GetMetrics` signature to accept `Coordinator` (or use global accessor)
- [ ] Integration tests: 3-node cluster, kill metrics controller → verify failover

## Phase 3 — Distributed Replication Propagation
- [ ] Modify `src/server/replication.go`:
  - Remove `if 0 == serverId` check in `ChangeReplicationModeServer`
  - All nodes that receive `ChangeReplicationMode` RPC propagate via gossip
  - Add `propagateViaGossip(replicationJson []byte)` function
- [ ] Create `src/coordination/propagation.go`:
  - `BroadcastReplicationMode(json []byte, originNodeID int32)` — gossip broadcast
  - Deduplication: track processed `mode:timestamp:origin` in LRU cache
  - Acknowledgment tracking with timeout
- [ ] Wire gossip broadcast in `bootstrapP2P` (server.go):
  - Register gossip handler for replication mode messages
  - On receive: apply mode change locally, ack to originator
- [ ] Update `ChangeReplicationModeClient` to use new propagation
- [ ] Integration tests: 3-node cluster, any node receives mode change → all nodes update

## Phase 4 — State Persistence + Handoff
- [ ] Implement BoltDB `replication_state` bucket in `storage.go`
  - Add `bucketReplicationState = []byte("replication_state")`
  - `PutReplicationState`, `GetReplicationState` methods on `CASStore`
- [ ] State persistence interval:
  - Background goroutine in `Coordinator` runs every `StatePersistenceInterval`
  - Persists all role states atomically
- [ ] Graceful handoff on lease release:
  - `Resign(role)` calls `PersistState` before releasing lease
  - New lease holder calls `LoadState` on acquisition
- [ ] Unit tests: `coordination_state_test.go` (persist/load, concurrent access)

## Phase 5 — Integration + Backward Compatibility
- [ ] Wire `Coordinator` in `momo.go` / `Daemon`:
  - Create `Coordinator` in `Daemon` if `coordination.enabled`
  - Pass to `GetMetrics`, `ChangeReplicationModeServer`
- [ ] Backward compatibility:
  - If `coordination.enabled=false`: legacy behavior (node 0 only)
  - Preserve `serverId != 0` early returns behind feature flag
- [ ] Update `momo.go` to start `Coordinator` background loops
- [ ] Update `bootstrapP2P` to register coordination gossip handlers
- [ ] Config validation: require P2P enabled if coordination enabled

## Phase 6 — Testing + Chaos
- [ ] Integration test: 3-node cluster, kill node 0 → verify metrics controller failover
- [ ] Integration test: any node receives mode change → all nodes update within 5s
- [ ] Chaos test: repeated kill of lease holder → cluster self-heals
- [ ] Benchstat: replication mode switch latency (target <100ms)
- [ ] Chaos test: network partition → verify no split-brain (lease quorum)
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with `[coordination]` section
- [ ] Update `docs/ARCHITECTURE.md` with distributed coordination section
- [ ] Blog post per Rule 76 (teach the lease-based election pattern)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [ ] PR body with `Resolves #1144`

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos test: kill coordinator → cluster self-heals
- Benchstat: no regression on coordination ops
- Reviewer ✅ gate (Rule 55)