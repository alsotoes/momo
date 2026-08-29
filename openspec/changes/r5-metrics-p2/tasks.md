# Tasks: R5 — Metrics phases 2–4 + dashboards/alerts (#933)

## 1. Phase 2: Storage/CAS metrics
- [x] `momo_blob_count` + `momo_stored_bytes_total` via scrape-time bbolt read (`CASStore.Stats`)
- [x] `momo_disk_used_bytes` / `momo_disk_free_bytes` via `syscall.Statfs` on data dir
- [x] `momo_dedup_hits_total` (server-side dedup hit counter)
- [x] `momo_cas_gc_runs_total` + `momo_cas_gc_evicted_bytes` (GC sweep + CVE-006 immediate eviction)
- [x] Tests: Stats() accuracy; GC counter accumulation; zero-gauges-without-provider

## 2. Phase 3: Replication + P2P metrics
- [x] `momo_replication_bytes_total` + `momo_replication_failures_total` (chain + splay forwarding)
- [x] `momo_cluster_peers`, `momo_swim_alive/suspect/offline_count` (+ `StateCount`, `AvgPingLatencySeconds`)
- [x] `momo_swim_ping_latency_seconds` (EWMA mean of alive peers)
- [x] `momo_leases_active` (`LeaseManager.ActiveLeases`)
- [x] `momo_scatter_queries_total` + `momo_scatter_timeout_total` (`ScatterGather` counters)
- [x] Tests: p2p gauge accuracy, scatter counters, lease gauge, no-provider zeros

## 3. Phase 4: Latency histograms (opt-in)
- [x] `enable_latency_histograms` config (default false) + docs
- [x] Fixed-bucket atomic histogram (no mutex / no client_golang)
- [x] `momo_request_latency_seconds{operation=upload|download|delete|list}`
- [x] `momo_replication_latency_seconds`
- [x] Zero-overhead gating (LatencyEnabled() + guarded time.Now)
- [x] Tests: opt-in, disabled zeros, bucket cumulative invariant, atomic

## 4. Dashboards / alerts
- [x] Grafana dashboard panels for storage + cluster gauges
- [x] Prometheus alerts (disk pressure, SWIM alive drop, scatter timeout, replication failure, lease starvation, blob growth)

## 5. Validation
- [x] `make build` + `make test` green (all modules)
- [x] Live smoke: boot daemon with histograms enabled, curl `/metrics` (blob/disk/cluster/swim/lease/histogram lines present)
- [x] Update `docs/CONFIGURATION.md` + `docs/TESTING.md` metric tables
- [x] Blog post per Rule 76 (`docs/blog/posts/026-metrics-observability.md` updated)
- [x] OpenSpec change `r5-metrics-p2` (proposal/spec/tasks, links #933)