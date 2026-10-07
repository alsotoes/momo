# Tasks — Coordinator Failover (SWIM-Based Leadership Election)

## Phase 1 — Core Infrastructure: SWIM-Integrated Lease Failure Detection
- [ ] Extend `src/p2p/lease.go` — `LeaseManager` with SWIM hooks
  - Add `OnPeerStateChange(peerID int32, state PeerState)` callback
  - `RevokeLease(peerID int32, role RoleType)` — immediate revocation
  - Register callback in `Gossiper` consumer loop for SUSPECT/OFFLINE events
- [ ] Modify `src/p2p/gossip.go`:
  - Add `OnPeerStateChange` callback registration
  - Call callback on `PeerState` transitions to SUSPECT/OFFLINE
- [ ] Unit tests: `lease_failover_test.go` (mock SWIM, verify revocation)

## Phase 2 — Proactive Leadership Handoff
- [ ] Extend `src/coordination/election.go` — `Coordinator` health monitoring
  - `StartHealthMonitor(ctx, interval)` — background goroutine
  - `CheckHealth() bool` — evaluates CPU, memory, goroutines
  - `ResignAll()` — graceful resignation of all held leases
- [ ] Health check implementation:
  - `GetCPUUsage() float64` — from `gopsutil`
  - `GetMemoryUsage() float64` — from `gopsutil`
  - `GetGoroutineCount() int` — `runtime.NumGoroutine()`
- [ ] Graceful resignation:
  - `Resign(role RoleType)` → persist state, broadcast intent, release lease
  - `ResignAll()` → resign all held roles
- [ ] Config parsing: `[coordination_failover]` section in `config.go`
- [ ] Unit tests: `coordinator_health_test.go` (mock health, verify resignation)

## Phase 3 — Split-Brain Prevention (Quorum)
- [ ] Extend `src/p2p/lease.go` — quorum-based lease acquisition
  - `AcquireLeaseWithQuorum(role RoleType) (bool, error)`
  - `GetQuorumSize() int` — `(aliveCount/2)+1`
  - `HasQuorum() bool` — check alive peer count
- [ ] Modify `LeaseManager.AcquireLease`:
  - If `split_brain_prevention=true`: call `AcquireLeaseWithQuorum`
  - If quorum not met: return error, schedule retry
- [ ] Quorum loss handling:
  - Monitor alive peer count in `PeerMap`
  - If quorum lost: release all leases, log "coordination suspended"
  - If quorum restored: resume normal election
- [ ] Unit tests: `lease_quorum_test.go` (mock peers, verify quorum logic)

## Phase 4 — State Synchronization Protocol
- [ ] Create `src/coordination/state_sync.go`:
  - `SyncStateRequest` / `SyncStateResponse` P2P RPC types
  - `RequestStateSync(peers []int32) (State, error)` — fan-out to peers
  - `MergeState(local, remote State) State` — latest timestamp wins
  - `ValidateState(state State) error` — bounds, timestamp, order checks
- [ ] Wire P2P RPC:
  - Register `MsgSyncStateRequest` / `MsgSyncStateResponse` in `metadata_rpc.go`
  - Handler in `MetadataRPCProvider` (or new `StateSyncProvider`)
  - Timeout handling: `state_sync_timeout` bounds fan-out
- [ ] State validation:
  - `currentIndex` in [0, len(replicationOrder)-1]
  - `startTime` not future, not > 24h old
  - `replicationOrder` valid permutation
  - On validation failure: safe defaults (index 0, now)
- [ ] Integration tests: new holder syncs state from peers

## Phase 5 — Observability + Integration
- [ ] Prometheus metrics in `src/server/metrics_exporter.go`:
  - `momo_coordination_lease_holder{role="..."}` — gauge
  - `momo_coordination_leadership_changes_total{role="..."}` — counter
  - `momo_coordination_state_sync_duration_seconds` — histogram
  - `momo_coordination_quorum_status` — gauge (1/0)
- [ ] Audit logging:
  - Leadership change: "Node X became MetricsController lease holder"
  - Resignation: "Node X resigned MetricsController: CPU 95%"
  - Quorum loss: "Coordination suspended: insufficient quorum"
- [ ] Config integration: `[coordination_failover]` section in `config.go`
- [ ] Backward compatibility: `coordination_failover.enabled=false` → standard lease
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with `[coordination_failover]` section

## Phase 6 — Integration Tests + Chaos
- [ ] Integration test: 3-node cluster, kill lease holder → verify failover < 5s
- [ ] Integration test: network partition → verify split-brain prevention
- [ ] Integration test: health degradation → verify proactive resignation
- [ ] Integration test: quorum loss → coordination suspended, resumes on restore
- [ ] Chaos test: repeated lease holder kills → cluster self-heals
- [ ] Benchstat: failover latency (target < 5s), state sync latency (target < 10s)
- [ ] `coordination_failover.enabled=false` → standard lease behavior
- [ ] Blog post per Rule 76 (teach the SWIM-integrated failover pattern)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [ ] PR body with `Resolves #1147`

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos tests pass: failover, partition, health degradation
- Benchstat: failover < 5s, state sync < 10s
- `coordination_failover.enabled=false` → standard lease behavior
- Reviewer ✅ gate (Rule 55)