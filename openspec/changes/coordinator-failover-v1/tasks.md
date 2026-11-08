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

## Phase 5 — Observability (delivered) + Integration
- [x] `momo_coordination_handoffs_total` — proactive lease resignations (counted
  only when leases were actually released)
- [x] `momo_coordination_quorum_loss_total` — coordination suspensions caused by
  cluster quorum loss
- [x] AUDIT logging: handoffs (`resigning lease "<key>" (<reason>)`) and quorum
  loss (`coordination suspended: cluster quorum lost after peer N failed`)
- [x] `momo_leases_active` reports held leases (R5)
- [x] Config integration: `[coordination_failover]` parsed, validated, and
  documented (Phase 2)
- [x] Backward compatibility: `coordination_failover.enabled=false` keeps the
  standard lease behaviour (no monitor starts)
- [x] `docs/GUIDES/CONFIGURATION.md` documents every `[coordination_failover]` key
- [ ] Per-role gauges/labels (`momo_coordination_lease_holder{role}`,
  `momo_coordination_leadership_changes_total{role}`) — roles are keyed leases
  today; per-role labels need a role taxonomy first
- [ ] `momo_coordination_state_sync_duration_seconds` — belongs to Phase 4
  (state sync), which is not implemented
- [ ] `momo_coordination_quorum_status` gauge — derivable from `momo_leases_active`
  and the quorum counters; add if dashboards need an explicit 1/0
- [ ] Dashboard panels / alert rules for the new counters — monitoring config,
  not code
