---
title: "Metrics and Observability: Per-Node Bind, Prometheus Export"
date: 2026-08-25T06:12:59Z
draft: false
tags: [go, metrics, prometheus, observability, bolt]
categories: [metrics]
summary: "Prometheus metrics export with a per-node metrics_host/metrics_port bind — observability that scales past the 'just scrape node 0' era."
artifacts:
  - {type: pr, id: "942"}
  - {type: pr, id: "895"}
  - {type: issue, id: "933"}
  - {type: spec, path: openspec/changes/metrics-per-node-binding}
  - {type: spec, path: openspec/changes/add-metrics-exporter}
  - {type: spec, path: openspec/changes/r5-metrics-p2}
related:
  - 024-bolt-performance-engineering
  - 015-sentinel-security-audit
---
# Metrics and Observability: Per-Node Bind, Prometheus Export

Momo was born "the metrics-driven controller" ([002](002-replication-strategies-polymorphic.md))
— so its own observability had to be first-class. The metrics exporter
(add-metrics-exporter) makes Prometheus endpoints real; **per-node binding**
(#942) is the scaling fix. **R5 (#933)** completed the operational surface:
storage/CAS gauges, P2P cluster gauges, opt-in latency histograms, and the
dashboards/alerts to consume them.

## The story

- **Exporter**: Prometheus-format `/metrics` per daemon (add-metrics-exporter),
  storage/CAS + P2P gauges, gathered without heap pressure (`os.Hostname`
  cached once, #895 — a bolt-grade micro-fix).
- **Per-node bind** (#942): `metrics_host`/`metrics_port` per node, so a
  multi-node ring is scrape-able at each server instead of converging on node
  0. Ratified as `openspec/changes/metrics-per-node-binding/`.

## R5: phases 2–4 (openspec/changes/r5-metrics-p2/)

- **Storage/CAS gauges** (scrape-time only — never hot-path): `momo_blob_count`,
  `momo_stored_bytes_total` (one bbolt read), `momo_disk_used_free_bytes`
  (Statfs on the data dir), `momo_cas_gc_runs_total` / `momo_cas_gc_evicted_bytes`
  (including CVE-006 immediate eviction on refcount-0 delete).
- **Replication + P2P gauges**: `momo_replication_bytes_total`,
  `momo_replication_failures_total`, `momo_cluster_peers`,
  `momo_swim_alive/suspect/offline_count`, `momo_swim_ping_latency_seconds`
  (mean EWMA RTT), `momo_leases_active`, `momo_scatter_queries_timeout_total`.
- **Opt-in latency histograms**: `enable_latency_histograms = true` arms
  `momo_request_latency_seconds{operation=upload|download|delete|list}` and
  `momo_replication_latency_seconds` — fixed-bucket, `sync/atomic` only.
  **Default off = zero overhead** (no `time.Now`, no bucket increments).
- **Dashboards/alerts**: Grafana panels for the new gauges; Prometheus alerts
  for disk pressure, SWIM alive-drop, scatter timeouts, replication failures,
  lease starvation, and blob-growth spikes.

## The Sentinel read

Metrics endpoints are an **unauthenticated surface**. Following Rule 75's
philosophy (no networked pprof on unauthenticated listeners), metric binding is
config-explicit — a dedicated listener, never a debug endpoint that exports
goroutines/runtime internals to the data path. If admin-profiling ever ships, it
must be loopback/Unix-socket + mTLS.

## ⚡ Bolt lens

- Zero-allocation metric collection (gauges aggregate on preallocated
  vertices).
- Storage gauges are computed **only at scrape** (single bbolt view + one
  Statfs), so the data path pays nothing.
- Histograms are atomic fixed-bucket and gate the timing capture behind
  `LatencyEnabled()` — the disabled path literally never allocates or reads a
  clock.
- Histograms/latency capture coincides with the benchmark history in
  `docs/PERFORMANCE.md`.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Controller origin: [002](002-replication-strategies-polymorphic.md). Perf arc:
[024](024-bolt-performance-engineering.md). Security: [015](015-sentinel-security-audit.md).