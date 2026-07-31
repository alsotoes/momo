# Proposal: Complete Prometheus Metrics Exporter

**Related Issue:** https://github.com/alsotoes/momo/issues/364

- **Champion:** opencode
- **Status:** `Draft`

## 1. Problem

PR #365 introduced a Prometheus metrics exporter (`src/server/metrics_exporter.go`) with 14 metrics, but 4 of 9 counters are dead code — the `MetricsCollector` is a local variable in `Daemon()` that is never plumbed into the transport, storage, replication, or P2P layers. As a result, `momo_downloads_total`, `momo_deletes_total`, `momo_replication_total`, and `momo_bytes_downloaded_total` permanently report 0. Additionally, `IncErrors` only covers 2 of ~12 error paths, and several important metric categories (storage, CAS, P2P, replication latency) are entirely missing.

## 2. Proposed Solution

Complete the metrics exporter in 4 phases, with strict overhead guarantees:

1. **Phase 1 — Fix wiring:** Inject `*MetricsCollector` into the transport layer via the existing interface assertion pattern (`SetMetricsHook`). Wire all dead-code counters and missing error paths. Zero new hot-path code.
2. **Phase 2 — Storage metrics:** Add disk usage and CAS metrics computed only at scrape time. Zero per-request overhead.
3. **Phase 3 — Replication + P2P metrics:** Add replication counters and P2P gauges. ~5ns per replication op.
4. **Phase 4 — Latency histograms (opt-in):** Request latency histograms gated behind config flag. ~40ns per request when enabled, zero when disabled.

## 3. Overhead Guarantees

- All counters/gauges use `sync/atomic` on `uint64`/`int64` — ~5ns per op, no locks, no allocations.
- Heavy operations (`runtime.ReadMemStats`, `syscall.Statfs`, bbolt reads) run only at scrape time (every 15-60s), never on the request hot path.
- Metrics HTTP server runs on a separate goroutine + port — does not share accept loop or semaphore with the main daemon.
- No `prometheus/client_golang` dependency (uses `sync.Mutex` + `float64` internally — 10-50x slower).
- Target: <1% throughput regression under k6 load test, <1ms scrape handler latency.

## 4. Validation Plan

1. Benchmark before/after (`go test -bench=.`) — target <1% ns/op regression.
2. k6 load test (10→20 VUs, 5 min) — target <1% throughput drop.
3. Scrape latency under load (1000 concurrent uploads) — target <1ms.
4. Memory overhead — target <1MB additional heap.
5. GC pause impact — target no measurable increase.
