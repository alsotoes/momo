# Change: Adaptive Thresholds — Self-Tuning Metrics + Pheromone Constants

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1145 (P0: Self-Tuning Thresholds + Pheromone Constants)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current Momo has **15+ hardcoded constants** that violate "Adapt, don't configure" (ADAPTIVE_SYSTEMS.md: Epigenetics) and Stigmergy:

| Location | Constant | Current Value | Should Adapt To |
|----------|----------|---------------|-----------------|
| `src/common/struct.go:177-178` | `MinThreshold`/`MaxThreshold` | Static config (e.g., 0.2/0.8) | Cluster CPU/mem history (EWMA) |
| `src/client/pheromone.go:15-28` | All pheromone constants | Fixed (1.0, 0.95, 0.2, 10.0, 1s) | Observed RTT variance, cluster size |
| `src/server/replication.go:21` | `payloadPoolCapacity` | 1024 | Workload (blob size distribution) |
| `src/server/replication.go:143` | `maxConcurrentConnections` | 1000 | Available file descriptors, RAM |
| `src/client/pheromone.go:27` | `EvaporationInterval` | 1s | Cluster dynamics, RTT distribution |
| `src/server/server.go:335` | Handshake timeout | 10s | Network RTT percentile |
| `src/server/server.go:431` | Metadata deadline | 60s | Object size + network |
| `src/server/replication.go:271` | Propagation timeout | 11s | Cluster size + latency |

These are **epigenetic markers** — the same binary should behave differently based on environment and runtime conditions, not static config.

## What Changes

### Phase 1: Adaptive Metrics Thresholds
- Replace static `MinThreshold`/`MaxThreshold` with **learned thresholds**
- Track EWMA of cluster CPU/mem over sliding window (configurable, default 1h)
- Auto-set thresholds at percentiles (e.g., MaxThreshold = P90 of observed usage)
- Persist learned thresholds to BoltDB for restart survival
- Fallback to config values if insufficient history

### Phase 2: Adaptive Pheromone Constants
- `InitialPheromone`: Set from median RTT of healthy nodes
- `EvaporationFactor`: Auto-tune from RTT variance (high variance → faster evaporation)
- `FailurePenaltyFactor`: Scale with observed failure recovery time
- `MaxPheromone`/`MinPheromone`: Bound by observed RTT range
- `EvaporationInterval`: Scale with cluster size (larger cluster → slower evaporation)

### Phase 3: Adaptive Pool Sizes + Timeouts
- `payloadPoolCapacity`: Track blob size distribution → size pool to P95 blob size
- `maxConcurrentConnections`: Scale with `ulimit -n` and available RAM
- Handshake timeout: P99 of observed handshake RTT + margin
- Metadata deadline: Object size / observed throughput + network RTT
- Propagation timeout: Cluster diameter × P99 RTT + margin

### Phase 4: Persistence + Restart Survival
- All learned values persisted to BoltDB (`adaptive_config` bucket)
- On restart: load learned values, continue adaptation
- Config values serve as safe defaults/fallbacks

### Phase 5: Config Integration
- New `[adaptive]` section in momo.conf
- Feature flag: `adaptive.enabled = true`
- Config values become **initial hints**, not fixed constants
- Observability: export learned values via `/metrics`

## Configuration

```toml
[adaptive]
enabled = true
learning_window = "1h"              # Sliding window for EWMA
threshold_percentile = 0.90         # MaxThreshold = P90 of usage
pheromone_learning_enabled = true   # Auto-tune pheromone constants
pool_sizing_enabled = true          # Auto-size pools
timeout_learning_enabled = true     # Auto-tune timeouts
persistence_interval = "30s"        # Persist learned values
min_history_samples = 100           # Min samples before adapting
```

## Non-Goals

- ML-based prediction (keep it simple: EWMA + percentiles)
- Cross-region adaptive thresholds (Phase 7)
- Per-tenant adaptive thresholds (R8)

## Impact

- **Principles**: Epigenetics (same binary, different behavior), Stigmergy (local adaptation), Adapt-don't-configure
- **Operations**: Zero manual threshold tuning; cluster self-tunes
- **Performance**: Auto-optimized timeouts/pools reduce latency + resource waste
- **Config**: 15+ hardcoded constants → 5 adaptive config keys with safe defaults

## Risk Mitigation

- **Bounded adaptation**: Learned values clamped to safe ranges (config min/max)
- **Fallback to config**: If learning fails or insufficient history
- **Gradual adaptation**: EWMA smoothing prevents oscillation
- **Observability**: All learned values exported via Prometheus
- **Feature flag**: `adaptive.enabled=false` → all static config