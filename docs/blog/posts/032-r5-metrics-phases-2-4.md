---
title: "R5 Metrics Phases 2-4: Storage, P2P, and Latency Histograms"
date: 2026-08-25T03:25:22Z
draft: false
post_type: architecture
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

A monitoring endpoint is only useful if people trust it. The first phase of our
metrics work wired a Prometheus exporter into the daemon; this is the follow-on
that made the numbers *worth looking at*: what the storage engine is holding,
whether the peer mesh is healthy, and — optionally — how slow our requests
actually are. The constraint on all of it was the same: observability must not
cost measurable throughput.

A quick recap of the metric types, since they drive every design choice below. A
**gauge** is a value that can go up or down (disk free, active peers). A
**counter** only increases (total bytes replicated). A **histogram** groups
observations into buckets so you can compute percentiles. Prometheus *scrapes*
these from a `/metrics` endpoint on an interval. **bbolt** is the embedded
key/value store that holds our object index; **statfs** is the operating-system
call that reports filesystem space. **Deduplication** means storing identical
content once and pointing many names at it; **CAS** is the content-addressed
store that makes that possible.

## The Real Problem

Phase 1 told us the process was alive. It did not tell us the things an operator
actually asks at 3 a.m.: is the disk filling? Are peers dropping out of the mesh?
Is replication stalling? Are our reads getting slower? Answering those required
either expensive per-request bookkeeping (which would slow the very requests we
were measuring) or nothing at all.

## Why the Obvious Solutions Failed

**"Instrument everything on the hot path."** A clock read and a bucket increment
on every request sounds harmless until you multiply it by every request. The
latency we added to measure latency would corrupt the measurement.

**"Pull in the standard Prometheus client library."** It is convenient, but its
floating-point counters and per-metric locking cost far more than the atomic
integer increments our access patterns need. We also did not want an external
dependency in the trust core.

**"Only measure storage; ignore the cluster."** A healthy disk on a node that
has been isolated from its peers is not a healthy node. Storage numbers without
peer-health numbers tell half a story.

## The Solution

Three phases, each with the same discipline: do the expensive work at scrape
time, and use lock-free atomics everywhere else.

### Phase 2 — Storage metrics, computed only when scraped

These read the object index and the filesystem *at scrape time* and never touch
the request path:

| Metric | Type | Source |
|--------|------|--------|
| `momo_blob_count` | Gauge | object index |
| `momo_stored_bytes_total` | Counter | sum of object sizes |
| `momo_disk_used_bytes` | Gauge | `statfs` |
| `momo_disk_free_bytes` | Gauge | `statfs` |
| `momo_gc_runs_total` | Counter | GC loop iterations |
| `momo_gc_evicted_total` | Counter | evicted objects |
| `momo_dedup_hits_total` | Counter | CAS deduplication hits |

### Phase 3 — Peer mesh and replication

These are read from live subsystem state at scrape time, with no locks and no
allocations:

| Metric | Type | Source |
|--------|------|--------|
| `momo_cluster_peers` | Gauge | alive peer count |
| `momo_swim_alive_count` | Gauge | peers seen as alive |
| `momo_swim_suspect_count` | Gauge | peers under suspicion |
| `momo_swim_offline_count` | Gauge | peers declared offline |
| `momo_swim_ping_latency_seconds` | Histogram | EWMA round-trip samples |
| `momo_leases_active` | Gauge | lease manager |
| `momo_scatter_queries_total` | Counter | scatter-gather dispatches |
| `momo_scatter_timeouts_total` | Counter | query timeouts |
| `momo_replication_total` | Counter | replication transfers |
| `momo_replication_bytes_total` | Counter | bytes replicated |

**SWIM** is the gossip protocol peers use to detect failure; **EWMA** is an
exponentially weighted moving average, which smooths round-trip samples so a
single slow ping does not distort the picture. **Scatter-gather** is a query
broadcast to many peers whose partial answers are merged.

### Phase 4 — Latency histograms, off by default

Latency tracking is gated behind a single configuration flag:

```ini
[metrics]
enable_latency_histograms = true
```

When it is off — the default — the timing code is never reached: no clock read,
no bucket increment, no allocation. When it is on, requests and replication
record into fixed-bucket histograms labeled by operation, using atomic
increments of roughly a few tens of nanoseconds per observation.

## Overhead Guarantees

| Guarantee | Mechanism |
|-----------|-----------|
| Under 1% throughput regression | atomic increments on plain integers; no locks, no allocations |
| Heavy work never on the hot path | memory stats, `statfs`, and index reads only at scrape |
| No interference with request serving | metrics run on their own goroutine and port |
| No heavyweight dependency | pure atomics instead of a floating-point metrics library |

## How We Verified

We validated the overhead claim three ways: a benchmark suite comparing
nanoseconds-per-op before and after; a load test ramping virtual users and
watching throughput; and scrape-latency measurement under a thousand concurrent
uploads, where the endpoint stayed responsive. Memory growth stayed in the low
single-digit megabytes.

## Failure Modes

| Risk | Guard |
|------|-------|
| Histogram overhead silently enabled | Default off; the disabled path is dead code at runtime |
| Scrape blocks request handling | Separate listener and goroutine |
| A metric reads a subsystem mid-mutation | Values are read from atomic state at scrape, not locked mid-write |
| Dashboards drift from the metrics | Panels and alerts are maintained alongside the metric definitions |

## Follow-ups

Grafana dashboards gained storage, peer, and replication panels, and Prometheus
alert rules cover disk pressure, peer loss, and replication stalls. The next
phase looks at custom bucket boundaries, exemplars, and a bridge to
OpenTelemetry for distributed tracing.

## Standards

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Spec: `openspec/changes/r5-metrics-p2`.
- Sibling posts: [026](026-metrics-observability.md),
  peer gossip [016](016-p2p-gossip-swim.md),
  scatter-gather and leases [017](017-scatter-gather-lease-quorum.md),
  performance engineering [024](024-bolt-performance-engineering.md).
- Performance history: [docs/REFERENCE/PERFORMANCE.md](../../PERFORMANCE.md).
