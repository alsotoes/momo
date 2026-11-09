# Tasks — Adaptive Thresholds (Self-Tuning Metrics + Pheromone Constants)

## Phase 1 — Core Infrastructure: Adaptive Config Framework
- [x] `ConfigurationAdaptive` in `src/common/struct.go` (Enabled, Alpha,
  MinSamples, PersistInterval) with the `[adaptive]` section parsed and validated;
  disabled by default so existing deployments are unchanged
- [x] Persistence: learned thresholds live in `<data_dir>/adaptive.db`
  (`adaptive_thresholds` bucket, separate file from the CAS store to avoid the
  bbolt single-writer trap) and are restored on start; `persist_interval=0`
  keeps them in memory only
- [x] `EWMALearner` in `src/metrics/adaptive.go` — alpha smoothing, protected
  variance, ignores NaN/Inf samples, thread-safe
- [x] Unit tests: learner mean/invalid samples, threshold derivation, hysteresis,
  persistence round trip, gauge snapshot, config parsing

## Phase 2 — Adaptive Metrics Thresholds
- [x] `AdaptiveThresholds` couples the learner to the controller; the loop feeds
  the memory-utilisation signal it acts on and uses the learned pair
- [x] Safety rails: the learned maximum never drops below the configured value,
  both values are clamped (max 60–95%, min 20–60%), the pair keeps a ≥10-point
  gap, and changes smaller than 5 points are ignored (no twitching)
- [x] Persistence is driven from the loop (`MaybePersist`) at the configured
  interval, and restored with the same clamps applied on load
- [x] Gauges: `momo_adaptive_max_threshold`, `momo_adaptive_min_threshold`,
  `momo_adaptive_threshold_samples`
- [x] Tests: pre-`min_samples` passthrough, quiet cluster keeps config, busy
  baseline raises max, clamps and gap, persistence/reload
- [ ] Integration test with real load variation — deferred: the unit suite covers
  the derivation and the loop wiring; a load-generation harness is a separate
  project

## Phase 3 — Adaptive Pheromone Constants
- [ ] Modify `src/client/pheromone.go`:
  - Add `AdaptivePheromone` struct wrapping constants + learner
  - `UpdateFromRTT(rtt time.Duration, success bool)` — feed RTT samples
  - `GetConstants() PheromoneConstants` — return adapted or config values
  - Replace package-level constants with dynamic getters
- [ ] RTT collection:
  - In `RecordSuccess`: record RTT sample for evaporation factor learning
  - In `RecordFailure`: record failure for penalty factor learning
  - Track RTT variance for evaporation factor calculation
- [ ] Pheromone constant bounds enforcement:
  - All computed values clamped to safe ranges (per spec table)
- [ ] Persistence:
  - Persist pheromone constants to `AdaptiveStore` under "pheromone_constants"
  - Load on `NewPheromoneRouter` creation
- [ ] Unit tests: `pheromone_adaptive_test.go` (constant adaptation from mock RTT)

## Phase 4 — Adaptive Pool Sizes + Timeouts
- [x] Adaptive replication payload pool: each broadcast feeds its real size to an
  EWMA learner; every 64 samples the pool target becomes `mean × 1.5`, clamped to
  `[1 KiB, 1 MiB]`. Only buffers with the current target capacity are returned to
  the pool, so a resize cannot poison it. (1 KiB floor, not the spec's 4 KiB:
  keeping the historical default avoids quadrupling the per-P footprint.)
  In-memory only — the learner reconverges in a few seconds, so persisting it
  would add a write path for no operational gain.
- [x] Adaptive connection limit: the environmental probe (FD limit, cgroup/RAM
  ceiling, clamped 32–10000) already existed and fed the accept loop, but
  `UpdateMetrics` was never called, so the limit stayed at boot capacity. The
  daemon now samples CPU/memory every 10s (`metrics.HostPressure`) and the
  controller sheds to 50% above 80% pressure and 25% above 90%.
- [x] Adaptive metadata RPC timeout: derived as 10× the cluster's measured mean
  RTT, clamped to [500ms, 10s]; an explicit override still wins and an unknown
  RTT falls back to the published default.
- [ ] Adaptive timeouts for handshake/propagation — deferred: those paths have
  fixed protocol deadlines tied to peer expectations, so adapt them only with a
  cross-version compatibility plan.
- [ ] Integration tests with real load — deferred to the multi-node harness.

## Phase 5 — Persistence + Observability + Config
- [x] Persistence: learned thresholds in `<data_dir>/adaptive.db`
  (`adaptive_thresholds` bucket, own file to avoid the bbolt single-writer trap),
  restored with clamps re-applied; no generic `adaptive_config` bucket needed.
- [x] Metrics: `momo_adaptive_max_threshold`, `momo_adaptive_min_threshold`,
  `momo_adaptive_threshold_samples` (the other features expose their state through
  existing gauges, e.g. peer load).
- [x] Config: `[adaptive]` section wired and validated (`alpha` in (0,1],
  `min_samples` ≥ 1, `persist_interval` ≥ 0).
- [x] `docs/GUIDES/CONFIGURATION.md` documents `[adaptive]`; `conf/momo.conf`
  carries a commented example.
- [x] `docs/ARCHITECTURE.md` describes adaptive thresholds, the replication pool,
  and the homeostatic connection limit.

## Phase 6 — Backward Compatibility + Testing
- [x] Feature flag enforcement: `adaptive.enabled=false` leaves thresholds
  static (no learner, no resolver, no persistence handle opened).
- [ ] Integration tests:
  - 3-node cluster, sustained load → verify thresholds adapt
  - Chaos: rapid load changes → verify no oscillation
  - Restart test: learned values persist across restart
- [ ] Benchstat: compare adaptive vs static (target: no regression)
- [x] Corruption fallback: a corrupt persisted value is reported, logged, and the
  controller starts from configuration (test pins it).
- [x] Blog post 066 (Rule 76) — EWMA baseline, clamps, dead zone, pool sizing.

## Cross-Phase Requirements
- [x] `go build ./...` clean at each phase
- [x] `go test ./...` passes at each phase
- [x] `go vet`, `gofmt -l` clean
- [x] `go work vendor` parity (Rule 25)
- [x] Blog post 066 (Rule 76)
- [x] ADR sync (Rule 78) — ADR 0017

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Benchstat: no regression on adaptive paths
- `adaptive.enabled=false` → zero behavioral change
- Reviewer ✅ gate (Rule 55)