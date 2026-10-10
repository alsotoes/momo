---
title: "Metrics and Observability: Per-Node Bind, Prometheus Export"
date: 2026-08-25T06:12:59Z
draft: false
post_type: architecture
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
  - 032-r5-metrics-phases-2-4
  - 055-adaptive-volume-storage
---

Momo was designed as a "metrics-driven controller" — a cluster that changes its
own replication topology in response to live telemetry. That made it
embarrassing when the cluster could not tell us how it was doing. We had log
lines, but no numbers: no way to see at a glance how many blobs we stored, how
much disk was left, or whether the peer mesh was healthy. This is the story of
making observability a first-class part of the daemon instead of an afterthought.

A short vocabulary check first, because the rest of the post leans on it.
**Prometheus** is a monitoring system that periodically *scrapes* a plain-text
`/metrics` endpoint on each server and stores the values as time series. A
**gauge** is a metric that can rise and fall (free disk bytes, active peers); a
**counter** only ever climbs (total bytes replicated); a **histogram** sorts
observations into buckets so we can compute percentiles such as p99. **Grafana**
is the dashboard tool we point at Prometheus to draw the charts.

## The Real Problem

Our first exporter answered "is the process up?" and little else. In a
multi-node ring, every operator scraped node 0, because that was the only address
wired into the config. Node 0's counters were a summary, not the truth: they
could not tell you which peer was sick, which disk was filling, or whether
replication was falling behind on a specific node. When something went wrong we
were reduced to grepping logs and guessing.

## Why the Obvious Solutions Failed

**"Scrape node 0 and be done."** Centralizing the scrape was simple, but it made
node 0 a single point of both failure and confusion. A metric that aggregates
across nodes hides the one node that is actually broken — exactly the node you
need to see.

**"Expose the standard Go runtime debug endpoint."** Go ships an endpoint that
dumps goroutine stacks and memory internals. It is wonderful for local debugging
and dangerous on a network: it leaks runtime internals to anyone who can reach
the port. We wanted metrics, not a live introspection surface.

**"Compute the expensive numbers on every request."** Reading the object index
and calling the filesystem for disk usage are cheap once, but ruinous if done per
request. Observability must not tax the data path it observes.

## The Solution

Two moves, one architectural and one about where work happens.

**Per-node binding.** Each daemon now gets its own metrics address — a
`metrics_host` and `metrics_port` in its configuration — so an operator (or
Prometheus itself, via service discovery) can scrape every server individually.
The ring becomes observable node by node instead of through one aggregate window.
The listener is a dedicated, config-explicit port: it is never the runtime debug
endpoint, so it cannot be used to dump goroutines or process internals. If we
ever ship admin profiling, it will be loopback-only or a Unix socket, gated
behind mutual TLS.

**Scrape-time collection.** Heavy reads happen only when `/metrics` is fetched,
not on the hot path. Storage numbers come from a single pass over the embedded
key/value store plus one filesystem `statfs` call. The host name, which is
constant for the process lifetime, is cached once instead of looked up on every
scrape.

The metrics themselves fall into three families:

- **Storage / content-addressed store** — blob count, stored bytes, disk
  used/free, and garbage-collection runs and evicted bytes. Eviction on a
  zero-reference delete is immediate, so a deleted blob cannot linger as an
  orphan.
- **Replication and peer mesh** — replicated bytes, replication failures, peer
  counts split into alive / suspect / offline, ping round-trip latency (an
  exponentially weighted moving average), active leases, and scatter-gather
  query timeouts. *SWIM* is the gossip protocol the cluster uses to detect
  failed peers; the alive/suspect/offline gauges are its view of the world.
- **Latency histograms (opt-in)** — request and replication latency buckets,
  armed only when `enable_latency_histograms` is set to `true`. When the flag is
  off, the timing code is never reached: no clock read, no bucket increment, no
  allocation.

> **Pattern: Scrape-Time Aggregation for Expensive Metrics**
> Compute metrics that require heavy reads (index scans, filesystem stats) only
> when the metrics endpoint is scraped, not on every request. Cache
> process-constant values at startup.
>
> **Applies when**: a metric's source is expensive to read but changes slowly
> relative to traffic.
> **Doesn't apply**: counters that must capture every event in real time — those
> belong on the hot path as atomic increments.

## How We Verified

The design goal was "observable without being felt." The latency histograms use
plain atomic integer increments rather than a floating-point metrics library, so
the disabled path costs nothing and the enabled path is a handful of
nanoseconds. Storage gauges are bounded by a single store read and one `statfs`.
We watched scrape latency under concurrent upload load to confirm the endpoint
stays responsive, and kept the numbers alongside the rest of the performance
history.

## Failure Modes

| Risk | Guard |
|------|-------|
| Metrics endpoint becomes an attack surface | Dedicated listener; no runtime/goroutine dump; profiling would be loopback + mTLS |
| Scraping every node floods the network | Scrape interval is the operator's choice; each response is a single bounded read |
| Histograms silently tax throughput | Default off; when on, atomic increments only |
| A node's metrics go stale | Per-node binding means each node reports for itself — no proxy to lie |

## When NOT to Use This

- **Single-node deployments** do not need per-node binding; one address is fine.
- **Debugging a live process** is not a metrics job — use a profiler on a
  loopback or Unix-socket endpoint, not the metrics port.
- **Sub-millisecond, per-event latency tracing** is better served by distributed
  tracing than by histogram buckets.

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Specs: `openspec/changes/add-metrics-exporter`,
  `openspec/changes/metrics-per-node-binding`,
  `openspec/changes/r5-metrics-p2`.
- Performance history: [docs/REFERENCE/PERFORMANCE.md](../../PERFORMANCE.md).
- Sibling posts: controller origin [002](002-replication-strategies-polymorphic.md),
  performance arc [024](024-bolt-performance-engineering.md),
  security audit [015](015-sentinel-security-audit.md),
  R5 phases 2–4 [032](032-r5-metrics-phases-2-4.md).
