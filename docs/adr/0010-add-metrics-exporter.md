# 0010-add-metrics-exporter

## Status
Accepted

## Confidence
High

## Context
PR #365 introduced a Prometheus metrics exporter (`src/server/metrics_exporter.go`) with 14 metrics, but 4 of 9 counters are dead code — the `MetricsCollector` is a local variable in `Daemon()` that is never plumbed into the transport, storage, replication, or P2P layers. As a result, `momo_downloads_total`, `momo_deletes_total`, `momo_replication_total`, and `momo_bytes_downloaded_total` permanently report 0. Additionally, `IncErrors` only covers 2 of ~12 error paths, and several important metric categories (storage, CAS, P2P, replication latency) are entirely missing.

## Decision
- All Operations Instrumented (Resolves #364): The server SHALL increment the appropriate counter for every file upload, download, delete, replication transfer, and error condition, regardless of which transport protocol or code path handles the operation.
- Storage Metrics at Scrape Time (Resolves #364): The server SHALL expose disk usage and CAS statistics computed only at scrape time, never on the request hot path.
- P2P and Cluster Metrics (Resolves #364): The server SHALL expose cluster topology and SWIM protocol metrics as gauges read from live state at scrape time.
- Latency Histograms Opt-In (Resolves #364): The server MAY expose request latency histograms when explicitly enabled via configuration. When disabled, there SHALL be zero overhead on the request path.
- Overhead Guarantees (Resolves #364): The metrics instrumentation SHALL NOT cause more than 1% throughput regression under load.
- No External Dependencies (Resolves #364): The metrics exporter SHALL NOT depend on `prometheus/client_golang` or any third-party metrics library. All counters and gauges SHALL use Go's `sync/atomic` package on integer types.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/026-metrics-observability.md

## References
- Issue: #364
- PR: #942
- Spec: `openspec/changes/add-metrics-exporter/`
- Blog: docs/blog/posts/026-metrics-observability.md

