> GitHub Issue URL: https://github.com/alsotoes/momo/issues/364

# Prometheus Metrics Exporter Specification

## Purpose
This specification defines the complete set of Prometheus metrics exposed by momo server nodes, the instrumentation wiring requirements, and the overhead guarantees that ensure the metrics thread does not impact cluster daemon performance.

## ADDED Requirements

### Requirement: All Operations Instrumented (Resolves #364)
The server SHALL increment the appropriate counter for every file upload, download, delete, replication transfer, and error condition, regardless of which transport protocol or code path handles the operation.

#### Scenario: Download via S3 GET
- **GIVEN** a client sends an HTTP GET request to an S3-compatible node
- **WHEN** the download completes successfully
- **THEN** `momo_downloads_total` increments by 1 and `momo_bytes_downloaded_total` increments by the number of bytes streamed

#### Scenario: Delete via S3 DELETE
- **GIVEN** a client sends an HTTP DELETE request
- **WHEN** the delete completes successfully
- **THEN** `momo_deletes_total` increments by 1

#### Scenario: Replication transfer
- **GIVEN** a primary node forwards a file to a secondary node via `connectToPeer`
- **WHEN** the replication transfer completes
- **THEN** `momo_replication_total` increments by 1 and `momo_replication_bytes_total` increments by the file size

#### Scenario: Error on any path
- **GIVEN** any error occurs (handshake failure, hash mismatch, storage error, placement failure, panic recovery)
- **WHEN** the error is handled
- **THEN** `momo_errors_total` increments by 1

### Requirement: Storage Metrics at Scrape Time (Resolves #364)
The server SHALL expose disk usage and CAS statistics computed only at scrape time, never on the request hot path.

#### Scenario: Prometheus scrapes /metrics
- **GIVEN** the metrics endpoint is configured and Prometheus scrapes `/metrics`
- **WHEN** the handler processes the request
- **THEN** the response includes `momo_disk_used_bytes`, `momo_disk_free_bytes`, `momo_blob_count`, and `momo_stored_bytes_total` computed via `syscall.Statfs` and bbolt reads at that moment

### Requirement: P2P and Cluster Metrics (Resolves #364)
The server SHALL expose cluster topology and SWIM protocol metrics as gauges read from live state at scrape time.

#### Scenario: Cluster state metrics
- **GIVEN** P2P is enabled and the node is participating in gossip
- **WHEN** Prometheus scrapes `/metrics`
- **THEN** the response includes `momo_cluster_peers`, `momo_swim_alive_count`, `momo_swim_suspect_count`, `momo_swim_ping_latency_seconds`, and `momo_leases_active` reflecting the current state

### Requirement: Latency Histograms Opt-In (Resolves #364)
The server MAY expose request latency histograms when explicitly enabled via configuration. When disabled, there SHALL be zero overhead on the request path.

#### Scenario: Histograms disabled (default)
- **GIVEN** `enable_latency_histograms` is not set or is false
- **WHEN** a file upload/download/delete is processed
- **THEN** no `time.Now()` calls or bucket increments occur for histogram tracking

#### Scenario: Histograms enabled
- **GIVEN** `enable_latency_histograms = true` in `[metrics]` section
- **WHEN** a file upload completes
- **THEN** `momo_request_latency_seconds{operation="upload"}` records the elapsed time into the appropriate fixed bucket via `sync/atomic` increment

### Requirement: Overhead Guarantees (Resolves #364)
The metrics instrumentation SHALL NOT cause more than 1% throughput regression under load.

#### Scenario: Overhead validation
- **GIVEN** a k6 load test of 10->20 VUs performing PUT operations for 5 minutes
- **WHEN** comparing throughput with metrics disabled vs. enabled
- **THEN** the throughput delta SHALL be less than 1%

#### Scenario: Scrape handler latency
- **GIVEN** 1000 concurrent file uploads are in progress
- **WHEN** Prometheus scrapes `/metrics`
- **THEN** the handler responds in less than 1ms (p99)

### Requirement: No External Dependencies (Resolves #364)
The metrics exporter SHALL NOT depend on `prometheus/client_golang` or any third-party metrics library. All counters and gauges SHALL use Go's `sync/atomic` package on integer types.

#### Scenario: Atomic-only implementation
- **GIVEN** any counter or gauge is incremented
- **WHEN** the increment executes
- **THEN** it uses `atomic.Uint64.Add` or `atomic.Int64.Add` -- no mutex locks, no float64 conversions, no heap allocations
