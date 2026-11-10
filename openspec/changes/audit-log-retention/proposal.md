# Change: Enforce Audit Log Retention

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1307 (tracking)

## Why

`[audit] retention_days` is parsed, validated (`>= 1`) and documented as how long
audit entries are retained before pruning, but nothing ever prunes. The
`audit_log` bucket therefore grows without bound: on a busy cluster every
authenticated S3 and native-protocol operation appends an entry, and the metadata
DB never reclaims them. That inflates `momo.db`, slows cursor scans over the log,
and silently breaks the retention promise the configuration makes.

The audit log is hash-chained for tamper evidence, so pruning cannot simply
delete rows: the first surviving entry references the hash of a deleted
predecessor and `VerifyAuditLog` would report a broken chain.

## What Changes

- Add `CASStore.PruneAuditLog(cutoff time.Time) (int, error)`, which deletes
  entries older than the cutoff in one bbolt write transaction and returns how
  many were removed.
- Add a new `audit_meta` bucket holding the hash of the last pruned entry
  (`pruned_through`). `VerifyAuditLog` seeds its chain check from that anchor, so
  the retained window stays verifiable while tampering inside it is still
  detected.
- Start `startAuditPruneLoop` from `Daemon` when the store is a CAS store and
  `retention_days > 0`: one pass at startup (so a node that was down catches up
  immediately), then every 24 hours, cancelled with the daemon context.
- Export `momo_audit_prune_runs_total` and `momo_audit_pruned_entries_total`, and
  emit an AUDIT log line per non-empty prune naming the count and the window.
- Document the durability trade-off: pruning deliberately weakens tamper
  evidence for entries older than the configured window.

## Impact

- Affected specs: `audit-log-retention` (new).
- Affected code: `src/storage/storage.go` (bucket, anchor, prune),
  `src/server/audit_retention.go` (loop), `src/server/server.go` (wiring),
  `src/server/metrics_exporter.go` (counters).
- Backward compatible: with `retention_days` at its default every existing
  deployment prunes only entries older than 365 days; a deployment that never
  enabled audit has an empty bucket and pays one cursor scan a day.
