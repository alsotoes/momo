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
- [ ] Adaptive connection limit — deferred: sizing accepts from `ulimit_n`/RAM
  changes the admission path (and needs a per-OS fallback), so it belongs in its
  own change with load testing rather than riding along with the pool sizing.
- [ ] Adaptive timeouts:
  - Track handshake RTT, metadata RTT, propagation RTT
  - Compute timeouts as P99 × safety_factor (clamped to safe ranges)
  - Persist to `AdaptiveStore` under "timeouts"
- [ ] Integration tests: verify pool sizing and timeouts adapt under load

## Phase 5 — Persistence + Observability + Config
- [ ] BoltDB `adaptive_config` bucket:
  - Add `bucketAdaptiveConfig = []byte("adaptive_config")` in `storage.go`
  - `PutAdaptiveConfig`, `GetAdaptiveConfig` methods on `CASStore`
- [ ] Metrics export:
  - Add all 8 adaptive gauges to `MetricsCollector`
  - Update in `SetStorageStats` / scrape path
- [ ] Config integration:
  - Wire `[adaptive]` section in `config.go`
  - Validate: `threshold_percentile` in (0,1), durations > 0
- [ ] Update `docs/GUIDES/CONFIGURATION.md` with `[adaptive]` section
- [ ] Update `docs/ARCHITECTURE.md` with adaptive thresholds section

## Phase 6 — Backward Compatibility + Testing
- [ ] Feature flag enforcement:
  - `adaptive.enabled=false` → all static config, no learning
  - Verify zero behavioral change when disabled
- [ ] Integration tests:
  - 3-node cluster, sustained load → verify thresholds adapt
  - Chaos: rapid load changes → verify no oscillation
  - Restart test: learned values persist across restart
- [ ] Benchstat: compare adaptive vs static (target: no regression)
- [ ] Chaos test: BoltDB corruption → falls back to static config
- [ ] Blog post per Rule 76 (teach the EWMA + percentile adaptation pattern)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — `make adr-sync`
- [ ] PR body with `Resolves #1145`

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Benchstat: no regression on adaptive paths
- `adaptive.enabled=false` → zero behavioral change
- Reviewer ✅ gate (Rule 55)