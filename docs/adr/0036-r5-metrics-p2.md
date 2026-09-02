# 0036-r5-metrics-p2

## Status
Accepted

## Confidence
High

## Context
The `/metrics` exporter (phase 1, #364) exposes transport/server counters but
no operational or cluster observability: operators cannot see CAS storage
health, dedup/GC behavior, SWIM membership, lease pressure, or latency. R5
closes that gap per the prod-ready roadmap (P1, #928): storage/CAS gauges, P2P
cluster gauges, opt-in latency histograms, and the dashboards/alerts to consume
them. Stated contract: scrape-only / atomic-only, zero hot-path overhead.

## Decision
- storage/CAS scrape-time gauges: The metrics exporter SHALL expose CAS storage gauges computed only at `/metrics` scrape time (never on the request hot path): `momo_blob_count`, `momo_stored_bytes_total` (from the CAS objects bucket), `momo_disk_used_bytes` and `momo_disk_free_bytes` (`syscall.Statfs` on the store data directory), plus `momo_dedup_hits_total`, `momo_cas_gc_runs_total`, and `momo_cas_gc_evicted_bytes` counters. A store that cannot provide these (e.g. a non-CAS backend) SHALL emit zero gauges so dashboards stay v...
- GC and dedup counters: The CAS GC path SHALL increment `momo_cas_gc_runs_total` per completed sweep and `momo_cas_gc_evicted_bytes` by the logical bytes removed (including bytes evicted immediately on refcount-0 delete, CVE-006). The server SHALL increment `momo_dedup_hits_total` once per content-addressable dedup hit.
- replication counters: The server SHALL track `momo_replication_bytes_total` (bytes actually forwarded) and `momo_replication_failures_total` (forwarding failures) on every chain/splay forwarding path.
- p2p cluster gauges: When P2P is enabled, the exporter SHALL expose scrape-time gauges from live state: `momo_cluster_peers`, `momo_swim_alive_count`, `momo_swim_suspect_count`, `momo_swim_offline_count`, `momo_swim_ping_latency_seconds` (mean EWMA RTT of alive peers), `momo_leases_active`, and counters `momo_scatter_queries_total` and `momo_scatter_timeout_total`. When P2P is disabled or no cluster provider is installed, these SHALL emit zero values.
- opt-in latency histograms: The `[metrics]` section SHALL support `enable_latency_histograms` (default false). When false, the request/replication paths SHALL incur zero timing overhead (no `time.Now()` captures, no bucket increments). When true, fixed- bucket atomic histograms SHALL record `momo_request_latency_seconds{operation= "upload|download|delete|list"}` and `momo_replication_latency_seconds`, implemented exclusively with `sync/atomic` per-bucket counters (no mutexes, no `prometheus/client_golang`).
- dashboards and alerts: The monitoring stack SHALL include Grafana dashboard panels and Prometheus alert rules covering the new storage and cluster metrics (disk pressure, SWIM alive drop, scatter timeouts, replication failures, lease starvation, blob growth). These are declarative artifacts only. ## UNCHANGED Behavior - Phase 1 counters (connections/uploads/downloads/deletes/replication/errors/ bytes, uptime, goroutines, memory, runtime GC, build info) are unchanged. - Metrics stay dependency-free (`sync/atomic` only)...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/r5-metrics-p2/
- Blog: docs/blog/posts/...md
