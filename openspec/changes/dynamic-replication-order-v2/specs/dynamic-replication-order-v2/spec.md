# Dynamic Replication Order — Runtime Optimization of Replication Mode Sequence

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1146

## Purpose

Replace static `ReplicationOrder` with a **self-optimizing sequence** that reorders replication modes based on observed performance, implementing **Stigmergy** (local performance → global order) and **Adapt-don't-configure**.

## Requirements

### Requirement 1: Per-Mode Performance Tracking

#### Scenario: Performance metrics collected per replication mode
**Given** the polymorphic controller runs with `replication_order.enabled=true`
**When** a replication mode is used for a transaction
**Then** the following metrics are recorded for that mode:
- Success count / total count → success rate
- Latency samples (P50, P95, P99)
- Throughput (bytes/sec)
- Error count by type

#### Scenario: Sliding window with EWMA
**Given** performance samples for each mode
**When** a new sample arrives
**Then** mode metrics are updated via EWMA (alpha = 0.1)
**And** raw samples kept in circular buffer (configurable size, default 1000)
**And** metrics decay for modes not recently used

#### Scenario: Performance persistence
**Given** `persistence_interval` elapses (default 30s)
**When** mode metrics have changed
**Then** metrics persisted to BoltDB (`replication_mode_perf` bucket)
**And** on restart, metrics loaded for warm start

### Requirement 2: Dynamic Reordering Algorithm

#### Scenario: Periodic reordering
**Given** `reorder_interval` elapses (default 10min)
**And** each mode has `min_samples_per_mode` (default 100)
**When** reordering triggers
**Then** modes are scored and reordered:

```
Score(mode) = w1 * success_rate + w2 * (1 / latency_p99) + w3 * throughput - w4 * error_rate
```

Weights configurable (default: w1=0.4, w2=0.3, w3=0.2, w4=0.1)

#### Scenario: Promotion rules
**Given** a mode's score is significantly higher than its predecessor
**When** `score(mode) > score(prev_mode) + promotion_margin` (default 0.1)
**And** mode has `min_samples_per_mode`
**And** `stability_window` elapsed since last move (default 30m)
**Then** mode swaps position with predecessor (moves earlier in order)

#### Scenario: Demotion rules
**Given** a mode's score is significantly lower than its successor
**When** `score(mode) < score(next_mode) - demotion_margin` (default 0.1)
**And** mode has `min_samples_per_mode`
**And** `stability_window` elapsed since last move
**Then** mode swaps position with successor (moves later in order)

#### Scenario: Semantic constraints
**Given** reordering is computed
**When** applying new order
**Then** the following constraints are enforced:
- `ReplicationNone` (4) never moves before `ReplicationChain` (1) or `ReplicationSplay` (2)
- `ReplicationPrimarySplay` (3) can move but preserves client-side semantics
- Order always contains all 4 modes exactly once
- No mode removed or duplicated

### Requirement 3: Integration with Polymorphic Controller

#### Scenario: Dynamic order used by metrics controller
**Given** `replication_order.enabled=true`
**When** `GetMetrics` runs `checkMetricsAndSwap`
**Then** it uses the current dynamic `replicationOrder` slice
**And** `currentIndex` refers to position in dynamic order
**And** durability floor (`MinimumDurabilityFactor`) still enforced

#### Scenario: Fallback timeout uses dynamic order
**Given** timeout fallback triggers in `GetMetrics`
**When** `currentIndex > 0`
**Then** fallback moves to `currentIndex - 1` in dynamic order
**And** durability floor checked for new mode

### Requirement 4: Per-Tenant/Bucket Override (Optional)

#### Scenario: Tenant-specific replication order
**Given** a tenant has `replication_order_override` in config
**When** that tenant's traffic is processed
**Then** the tenant's override order is used instead of global dynamic order
**And** override order subject to same semantic constraints
**And** fallback to global dynamic order if override incomplete

### Requirement 5: Configuration

```toml
[replication_order]
enabled = true
reorder_interval = "10m"
min_samples_per_mode = 100
promotion_margin = 0.1
demotion_margin = 0.1
stability_window = "30m"
persistence_interval = "30s"
weight_success_rate = 0.4
weight_latency = 0.3
weight_throughput = 0.2
weight_error_rate = 0.1
```

### Requirement 5: Persistence

#### Scenario: Performance metrics persist across restarts
**Given** `persistence_interval` elapses
**When** mode metrics have changed
**Then** all mode metrics persisted to BoltDB (`replication_mode_perf` bucket)
**And** on restart, metrics loaded for immediate reordering eligibility

### Requirement 6: Backward Compatibility

#### Scenario: Static order fallback
**Given** `replication_order.enabled=false` (default)
**When** daemon starts
**Then** static `ReplicationOrder` from config used
**And** no tracking, no reordering
**And** behavior identical to pre-dynamic version

## Configuration Schema

```go
type ConfigurationReplicationOrder struct {
	Enabled               bool          `ini:"enabled"`
	ReorderInterval       time.Duration `ini:"reorder_interval"`
	MinSamplesPerMode     int           `ini:"min_samples_per_mode"`
	PromotionMargin       float64       `ini:"promotion_margin"`
	DemotionMargin        float64       `ini:"demotion_margin"`
	StabilityWindow       time.Duration `ini:"stability_window"`
	PersistenceInterval   time.Duration `ini:"persistence_interval"`
	WeightSuccessRate     float64       `ini:"weight_success_rate"`
	WeightLatency         float64       `ini:"weight_latency"`
	WeightThroughput      float64       `ini:"weight_throughput"`
	WeightErrorRate       float64       `ini:"weight_error_rate"`
}
```

## BoltDB Schema

New bucket: `replication_mode_perf` (per-node)

```
Key: "Chain"        → {SuccessRate, LatencyP50, LatencyP95, LatencyP99, Throughput, ErrorRate, SampleCount, LastUpdated}
Key: "Splay"        → {Same fields}
Key: "PrimarySplay" → {Same fields}
Key: "None"         → {Same fields}
Key: "current_order" → [1,2,3,4] (dynamic order)
Key: "last_reorder"  → timestamp
```

## Observability

Prometheus gauges exported:
```
momo_replication_mode_score{mode="Chain"}           # Current score
momo_replication_mode_success_rate{mode="Chain"}    # Success rate
momo_replication_mode_latency_p99{mode="Chain"}     # P99 latency
momo_replication_mode_position{mode="Chain"}        # Position in order (1-4)
momo_replication_order_reorders_total               # Total reorders
```

## Acceptance Criteria

- [ ] Dynamic order differs from static config after sustained load
- [ ] Poorly performing modes demoted (move later in order)
- [ ] Well-performing modes promoted (move earlier in order)
- [ ] Semantic constraints enforced (None never before Chain/Splay)
- [ ] Stability window prevents thrashing
- [ ] Durability floor still enforced with dynamic order
- [ ] Metrics persist across restarts
- [ ] `replication_order.enabled=false` → static config behavior
- [ ] Benchstat: no regression on replication performance

## Failure Scenarios

### Insufficient Samples
```
1. New cluster, no mode has min_samples
2. Static config order used
3. As samples accumulate, modes become eligible for reordering
```

### Oscillation Prevention
```
1. Stability window (30m) prevents rapid re-reordering
2. Promotion/demotion margins (0.1) require significant score diff
3. Min samples (100) prevents noise-driven reordering
```

### Mode Failure
```
1. Mode has high error rate → score drops → demoted
2. If all replicated modes fail → None promoted to position 1
3. Durability floor prevents demotion below minimum durability
```

## Out of Scope

- Cross-region replication order (Phase 7)
- ML-based prediction (EWMA + thresholds sufficient)
- Changing mode semantics (Chain/Splay/PrimarySplay/None fixed)