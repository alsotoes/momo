> GitHub Issue URL: https://github.com/alsotoes/momo/issues/933

# r5-metrics-p2 Specification

## Purpose

Complete the Prometheus observability surface for momo's P1 operational track:
storage/CAS gauges, P2P/cluster gauges, opt-in latency histograms, and
dashboards/alerts — building on the phase 1 `/metrics` exporter + `MetricsHook`
(#364). All metrics honor the atomic-only, scrape-only, zero-hot-path-overhead
contract from the `add-metrics-exporter` spec.

## ADDED Requirements

### Requirement: storage/CAS scrape-time gauges

The metrics exporter SHALL expose CAS storage gauges computed only at `/metrics`
scrape time (never on the request hot path): `momo_blob_count`,
`momo_stored_bytes_total` (from the CAS objects bucket), `momo_disk_used_bytes`
and `momo_disk_free_bytes` (`syscall.Statfs` on the store data directory), plus
`momo_dedup_hits_total`, `momo_cas_gc_runs_total`, and
`momo_cas_gc_evicted_bytes` counters. A store that cannot provide these (e.g. a
non-CAS backend) SHALL emit zero gauges so dashboards stay valid.

#### Scenario: scrape a store with blobs
- **GIVEN** a CAS store holding N live blobs totaling B bytes and a scrape of
  `/metrics`
- **WHEN** the handler runs
- **THEN** `momo_blob_count N` and `momo_stored_bytes_total B` are emitted,
  plus disk used/free computed via Statfs on the data dir

#### Scenario: scrape without a storage provider
- **GIVEN** a metrics collector with no storage provider installed
- **WHEN** `/metrics` is scraped
- **THEN** `momo_blob_count 0`, `momo_stored_bytes_total 0`,
  `momo_disk_used_bytes 0`, `momo_disk_free_bytes 0` are emitted

### Requirement: GC and dedup counters

The CAS GC path SHALL increment `momo_cas_gc_runs_total` per completed sweep and
`momo_cas_gc_evicted_bytes` by the logical bytes removed (including bytes evicted
immediately on refcount-0 delete, CVE-006). The server SHALL increment
`momo_dedup_hits_total` once per content-addressable dedup hit.

#### Scenario: GC sweep evicts an orphaned blob
- **GIVEN** a deleted object whose blob is orphaned
- **WHEN** the CAS GC sweep runs (or a refcount-0 delete evicts immediately)
- **THEN** `momo_cas_gc_runs_total` increments by 1 and
  `momo_cas_gc_evicted_bytes` grows by the blob's size

### Requirement: replication counters

The server SHALL track `momo_replication_bytes_total` (bytes actually forwarded)
and `momo_replication_failures_total` (forwarding failures) on every chain/splay
forwarding path.

#### Scenario: chain forward succeeds
- **GIVEN** a chain-mode replication forward of S bytes
- **WHEN** the forward completes successfully
- **THEN** `momo_replication_bytes_total` increases by S

#### Scenario: splay forward fails
- **GIVEN** a splay-mode replication forward that errors
- **WHEN** the error is handled
- **THEN** `momo_replication_failures_total` increments

### Requirement: p2p cluster gauges

When P2P is enabled, the exporter SHALL expose scrape-time gauges from live
state: `momo_cluster_peers`, `momo_swim_alive_count`, `momo_swim_suspect_count`,
`momo_swim_offline_count`, `momo_swim_ping_latency_seconds` (mean EWMA RTT of
alive peers), `momo_leases_active`, and counters `momo_scatter_queries_total`
and `momo_scatter_timeout_total`. When P2P is disabled or no cluster provider is
installed, these SHALL emit zero values.

#### Scenario: p2p disabled
- **GIVEN** a collector without a cluster provider
- **WHEN** `/metrics` is scraped
- **THEN** all `momo_cluster_*`/`momo_swim_*`/`momo_leases_active`/
  `momo_scatter_*` gauges/counters emit 0

#### Scenario: mixed membership
- **GIVEN** a cluster with A alive, S suspect, and O offline peers
- **WHEN** `/metrics` is scraped
- **THEN** `momo_swim_alive_count A`, `momo_swim_suspect_count S`,
  `momo_swim_offline_count O`, and `momo_cluster_peers (A+S+O)` are emitted

### Requirement: opt-in latency histograms

The `[metrics]` section SHALL support `enable_latency_histograms` (default
false). When false, the request/replication paths SHALL incur zero timing
overhead (no `time.Now()` captures, no bucket increments). When true, fixed-
bucket atomic histograms SHALL record `momo_request_latency_seconds{operation=
"upload|download|delete|list"}` and `momo_replication_latency_seconds`,
implemented exclusively with `sync/atomic` per-bucket counters (no mutexes,
no `prometheus/client_golang`).

#### Scenario: histograms disabled (default)
- **GIVEN** `enable_latency_histograms` unset or false
- **WHEN** operations execute
- **THEN** the exporter emits zero-count histogram series and hot paths never
  call `time.Now()` for histogram purposes

#### Scenario: histograms enabled
- **GIVEN** `enable_latency_histograms = true`
- **WHEN** an upload finishes
- **THEN** `momo_request_latency_seconds_count{operation="upload"}` increments
  and the observed duration lands in the matching `le` bucket

### Requirement: dashboards and alerts

The monitoring stack SHALL include Grafana dashboard panels and Prometheus
alert rules covering the new storage and cluster metrics (disk pressure, SWIM
alive drop, scatter timeouts, replication failures, lease starvation, blob
growth). These are declarative artifacts only.

#### Scenario: alert rule present
- **GIVEN** the Prometheus config loaded with `tests/monitoring/prometheus`
- **WHEN** the rules are evaluated
- **THEN** the R5 alert rules (disk pressure, swim alive drop, scatter
  timeouts, replication failures) exist and reference real `momo_*` metrics

## UNCHANGED Behavior
- Phase 1 counters (connections/uploads/downloads/deletes/replication/errors/
  bytes, uptime, goroutines, memory, runtime GC, build info) are unchanged.
- Metrics stay dependency-free (`sync/atomic` only) and scrape-latency bounded
  (<1ms); storage gauges require one bbolt read + one Statfs per scrape.
- Histogram opt-in never alters request semantics — only adds timing when
  explicitly configured.
- P2P handling, lease/scatter semantics, and replication forwarding are
  unchanged by instrumentation.