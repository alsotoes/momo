# Change: R7 — Error Model + Ops (ENOSPC, Exit Codes, Cluster Health)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/935 (R7: Error model + ops)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current Momo error handling has three production-readiness gaps:

1. **No disk-full signaling** — `ENOSPC` is never emitted; writes silently fail or panic instead of returning a clean `ENOSPC` that clients/S3 SDKs can act on
2. **Single exit code** — all fatal exits use `exit 1`; no distinction between config error, network failure, disk full, etc., making automated orchestration/alerting impossible
3. **No cluster health introspection** — operators have no `/health` sub-endpoint that reports: node state, replication status, disk usage, peer membership, open leases, scrub progress

## What Changes

### ENOSPC Signaling
- `storage.go` `PutBlob` / `GetBlob` / `DeleteBlob` paths MUST check write-space before write and return `syscall.ENOSPC` cleanly when bbolt/file write fails with `ENOSPC`
- `local_blobstore.go` `PutBlob` checks `disk.FreeSpace()` before allocating; returns `ENOSPC` if below threshold
- S3 gateway maps `ENOSPC` → `507 Insufficient Storage` (per S3 spec)

### Distinct Exit Codes
- `main.go` `runServer` / `runClient` / `runFuseMount`:
  - `2` = config error (invalid config, missing required keys)
  - `3` = network error (port bind failed, TLS load failed)
  - `4` = storage error (disk full, bbolt corruption)
  - `5` = crypto error (key load, TLS handshake)
  - `6` = P2P error (gossip failure, lease loss)
  - `1` = generic/unclassified

### Cluster Health Introspection
- New `/health/detailed` endpoint (or extend `/health`) returning JSON:
  - `node_id`, `uptime`, `state` (healthy/degraded/critical)
  - `disk_used`, `disk_free`, `disk_percent`
  - `peers` (alive/suspect/offline counts)
  - `replication_mode`, `pending_replications`
  - `scrub_status` (running/idle, last_run, errors)
  - `leases_active`, `scatter_pending`

## Non-Goals

- Distributed tracing / OpenTelemetry (future observability tier)
- Structured logging overhaul (already `slog`-based)
- Alerting rules (Grafana/Prometheus rule files) — separate config

## Impact

- **Operability:** S3 clients get proper `507` on disk full; orchestration can distinguish failure modes via exit codes
- **Debugging:** Single `/health/detailed` call gives full cluster state
- **S3 Compliance:** `507 Insufficient Storage` per S3 spec