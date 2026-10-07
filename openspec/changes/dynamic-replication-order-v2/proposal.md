# Change: Dynamic Replication Order — Runtime Optimization of Replication Mode Sequence

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1146 (P1: Dynamic Replication Order Optimization)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)
- Extends: `dynamic-replication-factor/` (existing spec)

## Why

Current Momo uses a **static `ReplicationOrder`** parsed from config (`src/common/config.go:358-380`). This violates:

- **Multi-Scheme Replication** (Principle 4): Configurable per-tenant/bucket, but order is global and static
- **Stigmergy** (ADAPTIVE_SYSTEMS.md): Complex behavior from local rules, not central static config
- **Adapt, don't configure** (Epigenetics): Order should optimize based on observed mode performance

The replication order determines the polymorphic controller's degradation/promotion path. A static order means:
- Poorly performing modes stay in the sequence
- Well-adapted modes can't be promoted
- No per-workload optimization

## What Changes

### Phase 1: Performance Tracking per Mode
- Track per-mode metrics: success rate, latency, throughput, error rate
- Sliding window (configurable, default 1h) with EWMA smoothing
- Persist to BoltDB (`replication_mode_perf` bucket)

### Phase 2: Dynamic Reordering Algorithm
- Periodic reordering (configurable interval, default 10min)
- **Promotion**: Modes with high success rate + low latency move earlier in order
- **Demotion**: Modes with high error rate + high latency move later
- **Stability**: Minimum samples required before reordering (configurable)
- **Constraints**: Chain/Splay/PrimarySplay/None semantics preserved

### Phase 3: Per-Tenant/Bucket Ordering (Optional)
- Extend `TenantConfig` with `replication_order_override`
- Allow tenant-specific replication order based on workload profile
- Fallback to global dynamic order

### Phase 4: Integration with Polymorphic Controller
- `GetMetrics` uses dynamic order instead of static config
- Durability floor (`MinimumDurabilityFactor`) still enforced
- Fallback timeout uses dynamic order

## Configuration

```toml
[replication_order]
enabled = true
reorder_interval = "10m"
min_samples_per_mode = 100
promotion_threshold_success_rate = 0.99
demotion_threshold_error_rate = 0.05
promotion_threshold_latency_p99 = "500ms"
demotion_threshold_latency_p99 = "2s"
stability_window = "30m"          # Min time before reordering same mode
```

## Non-Goals

- Changing replication mode semantics (Chain/Splay/PrimarySplay/None)
- Cross-region replication order (Phase 7)
- ML-based prediction (keep EWMA + thresholds)

## Impact

- **Principles**: Stigmergy (local performance → global order), Adapt-don't-configure
- **Operations**: Self-optimizing replication strategy
- **Performance**: Degradation path adapts to actual mode performance
- **Config**: Static ReplicationOrder becomes initial hint