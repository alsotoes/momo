# 0010-add-metrics-exporter

## Status
Proposed

## Confidence
Low

## Context


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
- **Code**: Partial
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-metrics-exporter/
- Blog: docs/blog/posts/...md
