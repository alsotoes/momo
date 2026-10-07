# Adaptive Thresholds — Self-Tuning Metrics + Pheromone Constants

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1145

## Purpose

Replace **15+ hardcoded constants** with self-tuning adaptive values that learn from cluster runtime behavior, implementing the **Epigenetics** principle (ADAPTIVE_SYSTEMS.md): same binary, different behavior based on environment and history.

## Requirements

### Requirement 1: Adaptive Metrics Thresholds

#### Scenario: Threshold learning from cluster history
**Given** a cluster running with `adaptive.enabled=true`
**When** the metrics controller collects CPU/memory samples
**Then** it maintains an EWMA of cluster-wide usage over `learning_window`
**And** `MaxThreshold` is set to P90 (configurable `threshold_percentile`) of observed usage
**And** `MinThreshold` is set to P10 of observed usage
**And** thresholds update every `learning_window` interval

#### Scenario: Threshold fallback
**Given** insufficient history (`min_history_samples` not met)
**When** the metrics controller starts
**Then** it uses static config values (`MinThreshold`, `MaxThreshold` from momo.conf)
**And** begins collecting samples for learning

#### Scenario: Threshold persistence
**Given** learned thresholds have been updated
**When** `persistence_interval` elapses
**Then** thresholds are persisted to BoltDB (`adaptive_config` bucket)
**And** on restart, thresholds are loaded from BoltDB

### Requirement 2: Adaptive Pheromone Constants

#### Scenario: Pheromone constants auto-tuned from RTT observations
**Given** the pheromone router records RTT for each replica interaction
**When** sufficient RTT samples collected (`min_history_samples`)
**Then** the following constants are auto-tuned:

| Constant | Adaptation Formula |
|----------|-------------------|
| `InitialPheromone` | Median RTT of healthy nodes (normalized) |
| `EvaporationFactor` | `1 - (RTT_variance / RTT_mean)` clamped [0.90, 0.99] |
| `FailurePenaltyFactor` | `0.1 * (mean_recovery_time / RTT_mean)` clamped [0.05, 0.5] |
| `MaxPheromone` | `InitialPheromone * 10` (scaled by RTT range) |
| `MinPheromone` | `InitialPheromone * 0.1` |
| `EvaporationInterval` | `cluster_size * 0.5s` (larger cluster = slower evaporation) |

#### Scenario: Pheromone constant bounds
**Given** auto-tuned constants are computed
**When** constants are applied
**Then** they are clamped to safe ranges:

| Constant | Min | Max |
|----------|-----|-----|
| `EvaporationFactor` | 0.90 | 0.99 |
| `FailurePenaltyFactor` | 0.05 | 0.5 |
| `MaxPheromone` | 5.0 | 100.0 |
| `MinPheromone` | 0.01 | 1.0 |
| `EvaporationInterval` | 500ms | 30s |

### Requirement 3: Adaptive Pool Sizes

#### Scenario: Payload pool capacity adapts to blob size distribution
**Given** the server processes blobs of varying sizes
**When** `pool_sizing_enabled=true` and `min_history_samples` met
**Then** `payloadPoolCapacity` is set to P95 of observed blob sizes
**And** clamped to `[4096, 1048576]` (4KB - 1MB)

#### Scenario: Connection pool scales with resources
**Given** the server starts with `adaptive.enabled=true`
**When** `maxConcurrentConnections` is initialized
**Then** it is set to `min(ulimit_n / 4, available_RAM_MB * 10)`
**And** clamped to `[100, 10000]`

### Requirement 4: Adaptive Timeouts

#### Scenario: Timeouts learn from observed RTT
**Given** sufficient RTT samples collected
**When** `timeout_learning_enabled=true`
**Then** the following timeouts are auto-tuned:

| Timeout | Formula | Clamp |
|---------|---------|-------|
| Handshake timeout | P99(handshake_RTT) × 3 | [5s, 60s] |
| Metadata deadline | P99(metadata_RTT) × 5 | [10s, 300s] |
| Propagation timeout | cluster_diameter × P99(RTT) × 2 | [5s, 120s] |
| Evaporation interval | cluster_size × 0.5s | [500ms, 30s] |

### Requirement 5: Persistence + Restart Survival

#### Scenario: Learned values persist across restarts
**Given** `persistence_interval` elapses
**When** any adaptive value has changed
**Then** all learned values are persisted to BoltDB (`adaptive_config` bucket)
**And** on restart, values are loaded and adaptation continues

#### Scenario: Restart with no history
**Given** first run or BoltDB corrupted
**When** daemon starts with `adaptive.enabled=true`
**Then** all values initialized from static config (safe defaults)
**And** learning begins immediately

### Requirement 6: Observability

#### Scenario: Learned values exported via metrics
**Given** adaptive learning is active
**When** `/metrics` is scraped
**Then** the following gauges are exported:

```
momo_adaptive_max_threshold{role="metrics"}     # Current MaxThreshold
momo_adaptive_min_threshold{role="metrics"}     # Current MinThreshold
momo_adaptive_initial_pheromone                 # Current InitialPheromone
momo_adaptive_evaporation_factor                # Current EvaporationFactor
momo_adaptive_payload_pool_capacity             # Current payloadPoolCapacity
momo_adaptive_max_connections                   # Current maxConcurrentConnections
momo_adaptive_handshake_timeout_seconds         # Current handshake timeout
momo_adaptive_history_samples                   # Samples in learning window
```

### Requirement 7: Configuration

```toml
[adaptive]
enabled = true
learning_window = "1h"
threshold_percentile = 0.90
pheromone_learning_enabled = true
pool_sizing_enabled = true
timeout_learning_enabled = true
persistence_interval = "30s"
min_history_samples = 100
```

### Requirement 8: Backward Compatibility

#### Scenario: Legacy static config
**Given** `adaptive.enabled=false` (default)
**When** daemon starts
**Then** all constants use static momo.conf values
**And** no learning, no persistence, no metrics
**And** behavior identical to pre-adaptive version

## Configuration Schema

```go
type ConfigurationAdaptive struct {
	Enabled                   bool          `ini:"enabled"`
	LearningWindow            time.Duration `ini:"learning_window"`
	ThresholdPercentile       float64       `ini:"threshold_percentile"`
	PheromoneLearningEnabled  bool          `ini:"pheromone_learning_enabled"`
	PoolSizingEnabled         bool          `ini:"pool_sizing_enabled"`
	TimeoutLearningEnabled    bool          `ini:"timeout_learning_enabled"`
	PersistenceInterval       time.Duration `ini:"persistence_interval"`
	MinHistorySamples         int           `ini:"min_history_samples"`
}
```

## BoltDB Schema

New bucket: `adaptive_config` (per-node)

```
Key: "metrics_thresholds" → {MaxThreshold, MinThreshold, LastUpdated, SampleCount}
Key: "pheromone_constants" → {InitialPheromone, EvaporationFactor, FailurePenaltyFactor, MaxPheromone, MinPheromone, EvaporationInterval, LastUpdated, SampleCount}
Key: "pool_sizes" → {PayloadPoolCapacity, MaxConnections, LastUpdated, SampleCount}
Key: "timeouts" → {HandshakeTimeout, MetadataDeadline, PropagationTimeout, EvaporationInterval, LastUpdated, SampleCount}
```

## Acceptance Criteria

- [ ] MaxThreshold auto-adjusts to P90 of observed cluster CPU/mem usage
- [ ] Pheromone constants auto-tune from observed RTT variance
- [ ] Payload pool capacity tracks P95 blob size
- [ ] Timeouts adapt to observed network RTT percentiles
- [ ] All learned values persist across restarts
- [ ] Learned values exported via `/metrics` Prometheus gauges
- [ ] `adaptive.enabled=false` → identical to static config behavior
- [ ] Bounded adaptation: values never exceed safe clamping ranges
- [ ] Benchstat: no regression on latency/throughput with adaptation enabled

## Failure Scenarios

### Insufficient History
```
1. Cluster starts, no history
2. Uses static config values (safe defaults)
3. Collects samples until min_history_samples met
4. Begins adapting thresholds/constants
```

### Oscillation Prevention
```
1. EWMA smoothing on all learned values (alpha = 0.1)
2. Clamping to safe ranges prevents runaway
3. Minimum learning window (1h) prevents overfitting
```

### BoltDB Corruption
```
1. Adaptive config bucket unreadable
2. Falls back to static config values
3. Learning restarts from scratch
4. Logs warning for operator awareness
```

## Out of Scope

- ML-based prediction (keep EWMA + percentiles)
- Cross-region adaptive thresholds (Phase 7)
- Per-tenant adaptive thresholds (R8 multi-tenancy)
- Anomaly detection (separate: immune system model)