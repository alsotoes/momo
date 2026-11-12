# Tasks — Dynamic Replication Order Optimization

## Phase 1 — Core Infrastructure: Performance Tracking
- [x] Create `src/replication_order/tracker.go` — per-mode performance tracker
  - `ModeStats` struct: successRate, latencyNs, latencyP99, throughput, errorRate, score, samples
  - `Tracker` struct: map[mode]*ModeStats, EWMA alpha, minSamples, thread-safe with mutex
  - `Observe(mode int, result ModeResult)` — update EWMA stats, recompute score
  - `Order() []int` — modes sorted by composite score descending
  - `Score(mode int) float64` / `Stats(mode int) *ModeStats`
- [ ] Create `src/replication_order/store.go` — BoltDB persistence (deferred to Phase 4)
  - `replication_mode_perf` bucket, Save/Load metrics + order
- [x] Add `[replication_order]` section parsing in `config.go`
  - `ConfigurationReplicationOrder` struct: Enabled, ReevaluateSeconds, Alpha, MinSamples,
    SuccessWeight/LatencyWeight/ThroughputWeight/ErrorWeight
  - Default: `enabled=false` for backward compat (zero behavioral change)
  - Validation: weights sum to 1, intervals >= 1, alpha in (0,1]
- [x] Unit tests: `tracker_test.go` (EWMA scoring, score calculation, None-never-before-replicated constraint)

## Phase 2 — Dynamic Reordering Algorithm
- [x] Create `src/replication_order/reorder.go` — reordering logic
  - `Reorderer` wraps `Tracker` with a stability window (default 10 min)
  - `Order() []int` — recomputes only when the stability window has elapsed
  - `Observe` passthrough to the tracker
- [x] Semantic constraint enforcement (in `Tracker.Order()`):
  - `ReplicationNone` never before `ReplicationChain`/`ReplicationSplay`
  - All modes present exactly once
- [x] Unit tests: `reorder_test.go` (stability window, constraints, recompute after window)
- [ ] Background reordering goroutine (deferred): the metrics-controller tick
  (cfg.Metrics.Interval) re-reads `tracker.Order()` each step, which serves as the
  reorder cadence; a dedicated goroutine is not needed
- [ ] Promotion/demotion margins (deferred): the composite score already ranks
  modes; explicit margins are a refinement for Phase 4+

## Phase 3 — Integration with Polymorphic Controller
- [x] Modify `src/metrics/metrics.go`:
  - `metricsController.tracker *replication_order.Reorderer` (nil = static order)
  - `step()` refreshes `c.order` from `tracker.Order()` each tick
  - `switchTo()` observes push success/failure via `tracker.Observe`
  - `checkMetricsAndSwap` uses dynamic `c.order` for `currentIndex` bounds
  - Fallback timeout uses dynamic `c.order[currentIndex-1]`
- [x] Durability floor enforcement:
  - `shouldSwitchMode` still checks `effectiveModeDurability` for dynamic order
  - Dynamic order position does not bypass the durability floor
- [x] Initialize tracker in `GetMetrics` when `cfg.ReplicationOrder.Enabled`
- [ ] Integration tests (deferred): 3-node cluster; verify dynamic order affects mode switching (needs multi-node harness)

## Phase 4 — Persistence + Tenant Override (deferred)
- [ ] BoltDB `replication_mode_perf` bucket + Save/Load metrics + order
- [ ] Persistence interval background goroutine
- [ ] Tenant override: `TenantConfig.ReplicationOrderOverride`, per-tenant order

## Phase 5 — Observability + Config + Backward Compat (partially done)
- [x] Config integration: `[replication_order]` section wired and validated in `config.go`
- [x] Backward compatibility: `replication_order.enabled=false` → static config order
- [ ] Prometheus metrics export (`momo_replication_mode_score{mode}`, position, success rate)
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with `[replication_order]` section
- [ ] Update `docs/ARCHITECTURE.md` with dynamic replication order section

## Phase 6 — Testing + Validation (partially done)
- [x] `go build ./...` clean, `go test ./...` passes, `go vet`/`gofmt -l` clean, `go work vendor` parity
- [ ] Benchstat: compare dynamic vs static order
- [ ] Chaos test: rapid load changes → verify no thrashing (stability window)
- [ ] Blog post per Rule 76 (teach the stigmergic reordering pattern)

## Cross-Phase Requirements
- [x] `go build ./...` clean at each phase
- [x] `go test ./...` passes at each phase
- [x] `go vet`, `gofmt -l` clean
- [x] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [x] PR body with `Resolves #1146`

## Definition of Done (Per Phase)
- All phase tasks checked
- `replication_order.enabled=false` → zero behavioral change
- Reviewer ✅ gate (Rule 55)
