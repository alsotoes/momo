# Change: R5 — Metrics phases 2–4 + dashboards/alerts

**Related Issues:**
- https://github.com/alsotoes/momo/issues/933 (R5)
- https://github.com/alsotoes/momo/issues/928 (roadmap parent)
- https://github.com/alsotoes/momo/issues/364 (phase 1 exporter / MetricsHook parent)

## Why

The `/metrics` exporter (phase 1, #364) exposes transport/server counters but
no operational or cluster observability: operators cannot see CAS storage
health, dedup/GC behavior, SWIM membership, lease pressure, or latency. R5
closes that gap per the prod-ready roadmap (P1, #928): storage/CAS gauges, P2P
cluster gauges, opt-in latency histograms, and the dashboards/alerts to consume
them. Stated contract: scrape-only / atomic-only, zero hot-path overhead.

## What

Ratifies R5 phases 2–4 of the `add-metrics-exporter` spec and ships:

1. **Phase 2 — storage/CAS gauges** (scrape-time only):
   `momo_blob_count`, `momo_stored_bytes_total`, `momo_disk_used_bytes`,
   `momo_disk_free_bytes` (Statfs on the data dir), `momo_dedup_hits_total`,
   `momo_cas_gc_runs_total`, `momo_cas_gc_evicted_bytes`.
2. **Phase 3 — replication + P2P gauges**: `momo_replication_bytes_total`,
   `momo_replication_failures_total`, `momo_cluster_peers`,
   `momo_swim_alive_count`/`suspect_count`/`offline_count`,
   `momo_swim_ping_latency_seconds`, `momo_leases_active`,
   `momo_scatter_queries_total`, `momo_scatter_timeout_total`.
3. **Phase 4 — opt-in latency histograms**: `enable_latency_histograms`
   config; `momo_request_latency_seconds{operation=upload|download|delete|list}`
   and `momo_replication_latency_seconds`, fixed-bucket, `sync/atomic` only.
   Zero overhead when disabled (no time.Now/bucket increment).
4. **Dashboards/alerts**: Grafana panel set for the new metrics;
   Prometheus alerts (disk pressure, SWIM alive drop, scatter timeouts,
   replication failure, lease starvation, blob-growth spike).

## Out of scope
- Metrics phases 1 cleanup items already ratify separately (whole-file GET
  latency is not split per-op beyond the four operations above).
- Dashboards/alerts are declarative artifacts only — no Grafana provisioning
  daemon or alert-manager sender is introduced.
- R6+ (HA metadata, auth, audit) remain separate roadmap items.