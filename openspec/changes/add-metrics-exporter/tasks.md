## 1. Phase 1: Fix Wiring (connect dead code)
- [x] 1.1 Define `MetricsHook` interface in `src/transport/communicator.go` with methods: `IncDownloads()`, `IncDeletes()`, `AddBytesDownloaded(n uint64)`, `IncReplication()`, `IncErrors()`.
- [x] 1.2 Add `SetMetricsHook(hook MetricsHook)` method to `S3Communicator`, `MomoTCPCommunicator`, `MomoQUICCommunicator`.
- [x] 1.3 In `server.go` Daemon, inject collector via `comm.(interface{ SetMetricsHook(MetricsHook) })` assertion (same pattern as `SetStore`).
- [x] 1.4 Wire `IncDownloads` + `AddBytesDownloaded` in `s3_communicator.go` GET handler (after `io.Copy`, line ~347).
- [x] 1.5 Wire `IncDownloads` + `AddBytesDownloaded` in `momo_tcp.go` GET handler (line ~299).
- [x] 1.6 Wire `IncDownloads` + `AddBytesDownloaded` in `momo_quic.go` GET handler (line ~308).
- [x] 1.7 Wire `IncDeletes` in all three communicators' DELETE handlers.
- [x] 1.8 Wire `IncReplication` in `server.go` `connectToPeer` call sites (lines 383, 420).
- [x] 1.9 Wire `IncErrors` on all error return paths in `server.go` (handshake failure, metadata error, hash/filename/size validation, storage error, placement failure, getFile error).
- [ ] 1.10 Wire `IncErrors` in `file.go` `getFile` error paths.
- [ ] 1.11 Wire `IncErrors` in transport `HandshakeServer` error paths (auth failure, parse error).
- [x] 1.12 Fix `AddBytesUploaded` to only count actual bytes transferred (skip dedup hits).
- [x] 1.13 Write unit tests verifying counters increment correctly for upload, download, delete, error, replication paths.

## 2. Phase 2: Storage Metrics (scrape-only)
- [ ] 2.1 Add `momo_disk_used_bytes` and `momo_disk_free_bytes` gauges via `syscall.Statfs` on the data directory in the scrape handler.
- [ ] 2.2 Add `momo_blob_count` and `momo_stored_bytes_total` via bbolt read in the scrape handler.
- [ ] 2.3 Add `momo_dedup_hits_total` counter (atomic, incremented in `server.go` dedup check at line 300).
- [ ] 2.4 Add `momo_cas_gc_runs_total` and `momo_cas_gc_evicted_bytes` counters (atomic, incremented in CAS GC path).
- [ ] 2.5 Write tests for storage metrics accuracy.

## 3. Phase 3: Replication + P2P Metrics
- [ ] 3.1 Add `momo_replication_bytes_total` counter (atomic, in `connectToPeer`/`client.Connect`).
- [ ] 3.2 Add `momo_replication_failures_total` counter (atomic, on replication error paths).
- [ ] 3.3 Add `momo_cluster_peers` gauge (read from `PeerMap` on scrape).
- [ ] 3.4 Add `momo_swim_alive_count` and `momo_swim_suspect_count` gauges (read from SWIM state on scrape).
- [ ] 3.5 Add `momo_swim_ping_latency_seconds` gauge (EWMA from SWIM RTT, read on scrape).
- [ ] 3.6 Add `momo_leases_active` gauge (read from `LeaseManager` on scrape).
- [ ] 3.7 Add `momo_scatter_queries_total` and `momo_scatter_timeout_total` counters (atomic, in scatter-gather path).
- [ ] 3.8 Write tests for replication and P2P metrics.

## 4. Phase 4: Latency Histograms (opt-in)
- [ ] 4.1 Add `enable_latency_histograms` config option in `[metrics]` section (default: false).
- [ ] 4.2 Implement fixed-bucket histogram with `sync/atomic` per-bucket counters (no mutex).
- [ ] 4.3 Add `momo_request_latency_seconds{operation="upload|download|delete|list"}` histogram.
- [ ] 4.4 Add `momo_replication_latency_seconds` histogram.
- [ ] 4.5 Gate histogram recording behind config flag — zero overhead when disabled.
- [ ] 4.6 Write tests for histogram accuracy and config gating.

## 5. Validation
- [ ] 5.1 Run `go test -bench=.` before and after — document ns/op delta.
- [ ] 5.2 Run k6 load test (10→20 VUs, 5 min) with/without metrics — document throughput delta.
- [ ] 5.3 Measure `/metrics` handler latency under 1000 concurrent uploads — document p99.
- [ ] 5.4 Measure memory overhead — document heap delta.
- [ ] 5.5 Measure GC pause duration delta — document if measurable.
- [ ] 5.6 Update `docs/TESTING.md` with metrics validation results.
