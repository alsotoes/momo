# Tasks — Coordinator Failover (SWIM-Based Leadership Election)

## Phase 1 — Core Infrastructure: SWIM-Integrated Lease Failure Detection
- [x] `LeaseManager.OnPeerFailure(peerID)` — releases every held lease whose
  quorum is no longer satisfiable and broadcasts the release so a healthy node
  can take the role over immediately (conservative: a lease records the grant
  count, not the granters)
- [x] `Gossiper.OnPeerStateChange` — multi-observer hook fired on SUSPECT and
  OFFLINE transitions through a single `setPeerState` helper; recovery to ALIVE
  is not reported
- [x] Daemon wiring: gossip degradation events call `LeaseManager.OnPeerFailure`
- [x] Unit tests `lease_failover_test.go`: revocation on quorum loss, retention
  while quorum holds, self/unknown peer ignored, observer notification sequence,
  idempotent transitions
- [ ] `RevokeLease(peerID, role)` per-role API — not needed yet: roles are keyed
  leases (`metrics-controller`, `replication-propagator`), so the quorum rule
  supersedes a role-specific call

## Phase 2 — Proactive Leadership Handoff (delivered as a health monitor on the lease layer)
- [x] `src/p2p/health.go` — `HealthSnapshot`, `HealthLimits` (per-dimension
  disable via zero, startup-sample guard), injectable `HealthSampler`
- [x] Samplers: `health_linux.go` reads `/proc/stat` (CPU delta, not lifetime
  average) and `/proc/meminfo` (MemAvailable); `health_other.go` reports
  goroutines only, so CPU/memory thresholds never fire where unobservable.
  No new dependency: `gopsutil` was avoided in favour of a small /proc reader.
- [x] `LeaseManager.ResignAll(reason)` — releases and broadcasts every held
  lease with an AUDIT trail
- [x] `HealthMonitor` — interval sampling, cooldown against repeated
  resignations, context-cancellable `Run`
- [x] Config: `[coordination_failover]` (`enabled`, `interval`,
  `cpu_threshold`, `memory_threshold`, `goroutine_threshold`, `cooldown`) with
  validation and a P2P requirement
- [x] Daemon wiring: monitor started in `bootstrapP2P` when enabled
- [x] Tests: threshold matrix, disabled dimensions, startup-sample guard,
  resign-once-per-cooldown, healthy samples ignored, no-manager no-op, Run
  cancellation, config defaults/overrides/rejections
- [x] Documentation: `conf/momo.conf` sample, configuration guide, architecture

## Phase 3 — Split-Brain Prevention (Quorum)
- [x] `LeaseManager.SetClusterSize(n)` / `ClusterQuorum()` / `HasQuorum()` —
  quorum is a majority of the **configured** cluster (2 of 3, 3 of 5), not of
  whichever nodes happen to be reachable; unknown size (0) keeps the legacy
  reachable-peer heuristic for tests and embedded use
- [x] `Acquire` refuses to start in a minority partition
  (`EHOSTUNREACH`, "coordination suspended: cluster quorum lost")
- [x] Quorum loss releases **all** held leases (not just individually
  unsatisfiable ones) and audits the suspension
- [x] Daemon wires `SetClusterSize(len(daemons))`
- [x] Unit tests `lease_quorum_test.go`: quorum table, majority-cohort
  requirement, minority acquisition refused, suspension on quorum loss,
  single-node cluster unaffected
- [ ] Configurable `split_brain_prevention` toggle / retry scheduling — not
  needed: the guard is always on when the cluster size is known, and callers
  already retry acquisition on their own schedule
- [ ] Quorum-restored resume: no state machine needed; coordination resumes when
  the next `Acquire` succeeds

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