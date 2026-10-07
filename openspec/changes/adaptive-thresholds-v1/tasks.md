# Tasks — Adaptive Thresholds (Self-Tuning Metrics + Pheromone Constants)

## Phase 1 — Core Infrastructure: Adaptive Config Framework
- [ ] Create `src/adaptive/config.go` — adaptive configuration types
  - `ConfigurationAdaptive` struct with all feature flags
  - Default values matching spec
  - Validation: percentiles in [0,1], durations > 0
- [ ] Create `src/adaptive/store.go` — BoltDB persistence for learned values
  - `AdaptiveStore` interface: `Save(key string, value any)`, `Load(key string, dest any) error`
  - `adaptive_config` bucket with sub-keys per category
  - JSON encoding for complex structs
- [ ] Create `src/adaptive/learner.go` — generic EWMA learner
  - `EWMALearner` struct with alpha, window, minSamples
  - `AddSample(value float64)`, `GetPercentile(p float64) float64`, `GetMean() float64`
  - Thread-safe with mutex
- [ ] Add `[adaptive]` section parsing in `config.go`
  - Wire `ConfigurationAdaptive` into global config
  - Default: `enabled=false` for backward compat
- [ ] Unit tests: `adaptive_learner_test.go` (EWMA accuracy, percentile estimation)

## Phase 2 — Adaptive Metrics Thresholds
- [ ] Modify `src/metrics/metrics.go`:
  - Add `AdaptiveThresholds` struct: `maxThresh`, `minThresh`, `learner *EWMALearner`
  - `UpdateThresholds(cpuUsed, memUsed float64)` — feed samples to learner
  - `GetThresholds() (max, min float64)` — return learned or config values
  - Replace static `maxThreshPercent`/`minThreshPercent` with dynamic calls
- [ ] Threshold persistence:
  - Background goroutine in `GetMetrics` loop persists every `persistence_interval`
  - Save to `AdaptiveStore` under "metrics_thresholds"
  - On `GetMetrics` start: load thresholds from store
- [ ] Metrics export:
  - Add `momo_adaptive_max_threshold` and `momo_adaptive_min_threshold` gauges
  - Update in `MetricsCollector` scrape path
- [ ] Integration tests: 3-node cluster, vary load → verify thresholds adapt

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
- [ ] Adaptive payload pool:
  - Track blob size samples in `server.go` / `replication.go`
  - `payloadPoolCapacity` = P95 of observed sizes (clamped [4KB, 1MB])
  - Persist to `AdaptiveStore` under "pool_sizes"
- [ ] Adaptive connection limit:
  - `maxConcurrentConnections` = `min(ulimit_n/4, RAM_MB*10)` clamped [100, 10000]
  - Read `ulimit -n` at startup via `syscall.Getrlimit`
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