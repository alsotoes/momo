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
  - {type: spec, path: openspec/changes/metrics-per-node-binding}
  - {type: spec, path: openspec/changes/add-metrics-exporter}
related:
  - 024-bolt-performance-engineering
  - 015-sentinel-security-audit
---
# Metrics and Observability: Per-Node Bind, Prometheus Export

Momo was born "the metrics-driven controller" ([002](002-replication-strategies-polymorphic.md))
— so its own observability had to be first-class. The metrics exporter
(add-metrics-exporter) makes Prometheus endpoints real; **per-node binding**
(#942) is the scaling fix.

## The story

- **Exporter**: Prometheus-format `/metrics` per daemon (add-metrics-exporter),
  storage/CAS + P2P gauges, gathered without heap pressure (`os.Hostname`
  cached once, #895 — a bolt-grade micro-fix).
- **Per-node bind** (#942): `metrics_host`/`metrics_port` per node, so a
  multi-node ring is scrape-able at each server instead of converging on node
  0. Ratified as `openspec/changes/metrics-per-node-binding/`.

## The Sentinel read

Metrics endpoints are an **unauthenticated surface**. Following Rule 75's
philosophy (no networked pprof on unauthenticated listeners), metric binding is
config-explicit — a dedicated listener, never a debug endpoint that exports
goroutines/runtime internals to the data path. If admin-profiling ever ships, it
must be loopback/Unix-socket + mTLS.

## ⚡ Bolt lens

- Zero-allocation metric collection (gauges aggregate on preallocated
  vertices).
- Histograms/latency capture coincides with the benchmark history in
  `docs/PERFORMANCE.md`.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Controller origin: [002](002-replication-strategies-polymorphic.md). Perf arc:
[024](024-bolt-performance-engineering.md). Security: [015](015-sentinel-security-audit.md).