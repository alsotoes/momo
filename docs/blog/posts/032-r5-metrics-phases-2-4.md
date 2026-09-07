---
title: "R5 Metrics Phases 2-4: Storage, P2P, and Latency Histograms"
date: 2026-08-25T03:25:22Z
draft: false
tags: [metrics, observability, prometheus, bolt, sentinel]
categories: [metrics]
summary: "R5 Phases 2-4 shipped: storage metrics (disk, CAS, GC), P2P metrics (SWIM, leases, scatter-gather), and opt-in latency histograms — all with <1% overhead via sync/atomic counters."
artifacts:
  - {type: spec, path: openspec/changes/r5-metrics-p2}
  - {type: issue, id: "933"}
related:
  - 026-metrics-observability
  - 016-p2p-gossip-swim
  - 017-scatter-gather-lease-quorum
  - 024-bolt-performance-engineering
---
# R5 Metrics Phases 2-4

Phase 1 wired the exporter; Phases 2-4 complete the observability picture with storage internals, P2P cluster health, and opt-in latency histograms — all with strict <1% overhead guarantees.

## Phase 2: Storage Metrics (Scrape-Time Only)

Counters/gauges computed **only at scrape time** — zero per-request overhead:

| Metric | Type | Source |
|--------|------|--------|
| `momo_blob_count` | Gauge | Bbolt `Objects` bucket |
| `momo_stored_bytes_total` | Counter | Sum of object sizes |
| `momo_disk_used_bytes` | Gauge | `syscall.Statfs` |
| `momo_disk_free_bytes` | Gauge | `syscall.Statfs` |
| `momo_gc_runs_total` | Counter | GC loop iterations |
| `momo_gc_evicted_total` | Counter | Evicted objects |
| `momo_dedup_hits_total` | Counter | CAS deduplication hits |

All computed in `/metrics` handler via `syscall.Statfs` + bbolt reads — **never on hot path**.

## Phase 3: P2P & Replication Metrics

Live gauges from P2P subsystem state:

| Metric | Type | Source |
|--------|------|--------|
| `momo_cluster_peers` | Gauge | `PeerMap` alive count |
| `momo_swim_alive_count` | Gauge | `PeerMap` ALIVE peers |
| `momo_swim_suspect_count` | Gauge | `PeerMap` SUSPECT peers |
| `momo_swim_offline_count` | Gauge | `PeerMap` OFFLINE peers |
| `momo_swim_ping_latency_seconds` | Histogram | EWMA RTT samples |
| `momo_leases_active` | Gauge | Lease manager |
| `momo_scatter_queries_total` | Counter | Scatter-gather dispatches |
| `momo_scatter_timeouts_total` | Counter | Query timeouts |
| `momo_replication_total` | Counter | Replication transfers |
| `momo_replication_bytes_total` | Counter | Bytes replicated |

Read from live `PeerMap` / lease manager at scrape — **no locks, no allocations**.

## Phase 4: Latency Histograms (Opt-In)

Gated behind `enable_latency_histograms` in `[metrics]` config:

```ini
[metrics]
enable_latency_histograms = true
```

When **disabled (default)**: zero overhead — no `time.Now()`, no bucket increments.

When **enabled**: `momo_request_latency_seconds{operation="upload|download|delete|replication"}` with fixed buckets via `sync/atomic` increments (~40ns/op).

## Overhead Guarantees

| Guarantee | Mechanism |
|-----------|-----------|
| <1% throughput regression | `sync/atomic` on `uint64`/`int64` — ~5ns/op, no locks, no allocations |
| Scrape-time heavy ops | `runtime.ReadMemStats`, `syscall.Statfs`, bbolt reads ONLY at scrape (15-60s) |
| Separate HTTP server | Metrics on separate goroutine + port — no shared accept loop/semaphore |
| No `prometheus/client_golang` | Pure `sync/atomic` — 10-50x faster than `float64` library |

## Validation

- `make benchmark` — <1% ns/op regression
- k6 load test (10→20 VUs, 5 min) — <1% throughput drop
- Scrape latency under load (1000 concurrent uploads) — <1ms p99
- Memory overhead — <1MB additional heap

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md): ⚡ **Bolt** (atomic counters, zero allocations, scrape-time batching), 🛡 **Sentinel** (no external deps, bounded memory, fail-closed).

## Follow-ups

- Grafana dashboards updated (storage, P2P, replication panels)
- Prometheus alert rules: disk pressure, peer loss, replication stall
- Phase 5: Custom bucket boundaries, exemplars, OpenTelemetry bridge

## Artifacts

- Spec: `openspec/changes/r5-metrics-p2/`
- Issue: #933
- PR: #... (merged)
