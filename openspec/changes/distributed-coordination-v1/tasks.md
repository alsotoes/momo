# Tasks — Distributed Coordination (Metrics Controller + Replication Propagation)

## Phase 1 — Core Infrastructure: Lease Election for Coordination Roles
- [x] Election implemented in `src/server/coordination_elector.go` (kept in the
  server package where the `LeaseManager` and gossip live; a separate
  `src/coordination` package would only re-export them).
- [x] Campaign/renew/resign via the lease manager; peer-side renewal accepts the
  current holder's lease ID and denies a different holder, so renew never opens a
  mutual-exclusion gap.
- [x] State persistence reuses the per-node `CoordinationModeStore` (the mode is
  already persisted on every node and reloaded by `resumeMode`), so no separate
  `replication_state` bucket is needed for the metrics-controller role.
- [x] `[coordination]` config section: `enabled`, `metrics_controller_lease_ttl`
  (default 30 s, positive), requires `[p2p] enabled=true`.
- [x] Unit tests: acquire/renew/step-down/re-acquire, graceful release on stop,
  lease renewal semantics, and the metrics gate for any node ID.

## Phase 2 — Distributed Metrics Controller
- [x] `GetMetrics` no longer exits on `serverId != 0`: every node keeps the loop
  alive and consults `metrics.IsControllerNode`, which is lease-backed when the
  elector is installed and node-0-only otherwise.
- [x] Non-holders stand by on each tick; they persist and announce the resumed
  mode on takeover, so failover converges immediately.
- [x] Leadership loss steps the node back to standby without stopping the loop;
  it can re-acquire later.
- [x] Mode state: persisted with the existing per-node mode store and restored by
  `resumeMode` at loop start and on takeover.
- [ ] Integration test on a 3-node cluster (kill the holder) — needs the
  multi-node harness; covered at unit level here.

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