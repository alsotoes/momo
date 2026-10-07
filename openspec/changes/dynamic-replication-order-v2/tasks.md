# Tasks — Dynamic Replication Order Optimization

## Phase 1 — Core Infrastructure: Performance Tracking
- [ ] Create `src/replication_order/tracker.go` — per-mode performance tracker
  - `ModeMetrics` struct: successRate, latencyP50/P95/P99, throughput, errorRate, sampleCount
  - `Tracker` struct: map[mode]ModeMetrics, EWMA alpha, circular buffer
  - `Record(mode int, success bool, latency time.Duration, bytes int64, err error)`
  - `GetScore(mode int) float64` — compute weighted score
  - Thread-safe with mutex
- [ ] Create `src/replication_order/store.go` — BoltDB persistence
  - `replication_mode_perf` bucket
  - `SaveMetrics(mode int, metrics ModeMetrics)`, `LoadMetrics(mode int) (ModeMetrics, error)`
  - `SaveOrder(order []int)`, `LoadOrder() []int`
- [ ] Add `[replication_order]` section parsing in `config.go`
  - `ConfigurationReplicationOrder` struct with all feature flags
  - Default: `enabled=false` for backward compat
- [ ] Unit tests: `tracker_test.go` (EWMA scoring, score calculation)

## Phase 2 — Dynamic Reordering Algorithm
- [ ] Create `src/replication_order/reorder.go` — reordering logic
  - `Reorder(order []int, metrics map[int]ModeMetrics) []int`
  - `ComputeScore(metrics ModeMetrics) float64` — weighted formula
  - `Promote(order []int, metrics map[int]ModeMetrics) []int` — swap with predecessor
  - `Demote(order []int, metrics map[int]ModeMetrics) []int` — swap with successor
  - `EnforceConstraints(order []int) []int` — semantic constraints
- [ ] Background reordering goroutine:
  - Runs every `reorder_interval` (default 10min)
  - Checks `min_samples_per_mode` before reordering
  - Applies `stability_window` (default 30m) to prevent thrashing
  - Updates in-memory order, persists to BoltDB
- [ ] Semantic constraint enforcement:
  - `ReplicationNone` never before `ReplicationChain`/`ReplicationSplay`
  - All 4 modes present exactly once
  - `ReplicationPrimarySplay` preserves client-side semantics
- [ ] Unit tests: `reorder_test.go` (constraint enforcement, promotion/demotion logic)

## Phase 3 — Integration with Polymorphic Controller
- [ ] Modify `src/metrics/metrics.go`:
  - Replace static `replicationOrder` with dynamic order from tracker
  - `GetMetrics` accepts `*replication_order.Tracker` dependency
  - `checkMetricsAndSwap` uses dynamic order for `currentIndex` bounds
  - Fallback timeout uses dynamic order for `currentIndex - 1`
- [ ] Durability floor enforcement:
  - `shouldSwitchMode` still checks `effectiveModeDurability` for dynamic order
  - Dynamic order position doesn't bypass durability floor
- [ ] Initialize tracker in `momo.go` / `Daemon`:
  - Create tracker if `replication_order.enabled`
  - Pass to `GetMetrics` and `ChangeReplicationModeServer`
- [ ] Integration tests: 3-node cluster, verify dynamic order affects mode switching

## Phase 4 — Persistence + Tenant Override
- [ ] BoltDB `replication_mode_perf` bucket:
  - Add `bucketReplicationModePerf = []byte("replication_mode_perf")` in `storage.go`
  - `PutModeMetrics`, `GetModeMetrics`, `PutOrder`, `GetOrder` methods
- [ ] Persistence interval:
  - Background goroutine saves metrics + order every `persistence_interval`
- [ ] Tenant override (optional):
  - Extend `TenantConfig` with `ReplicationOrderOverride []int`
  - Config parsing for `[tenant.xxx] replication_order_override`
  - Override used in `server.go` when tenant identified
- [ ] Integration tests: verify persistence across restart, tenant override works

## Phase 5 — Observability + Config + Backward Compat
- [ ] Prometheus metrics export:
  - `momo_replication_mode_score{mode="Chain"}`
  - `momo_replication_mode_position{mode="Chain"}`
  - `momo_replication_mode_success_rate{mode="Chain"}`
  - `momo_replication_mode_latency_p99{mode="Chain"}`
  - `momo_replication_order_reorders_total`
- [ ] Config integration:
  - Wire `[replication_order]` section in `config.go`
  - Validate: weights sum to ~1.0, intervals > 0, margins in [0,1]
- [ ] Backward compatibility:
  - `replication_order.enabled=false` → static config order
  - Verify zero behavioral change when disabled
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with `[replication_order]` section
- [ ] Update `docs/ARCHITECTURE.md` with dynamic replication order section

## Phase 6 — Testing + Validation
- [ ] Integration tests:
  - 3-node cluster, sustained load → verify order changes
  - Inject errors in one mode → verify demotion
  - High performance mode → verify promotion
  - Restart test: order persists across restart
- [ ] Chaos test: rapid load changes → verify no thrashing (stability window)
- [ ] Benchstat: compare dynamic vs static order (target: improved adaptation)
- [ ] `replication_order.enabled=false` → zero behavioral change
- [ ] Blog post per Rule 76 (teach the stigmergic reordering pattern)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [ ] PR body with `Resolves #1146`

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Benchstat: no regression on replication performance
- `replication_order.enabled=false` → zero behavioral change
- Reviewer ✅ gate (Rule 55)