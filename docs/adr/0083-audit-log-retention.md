# 0083-audit-log-retention

## Status
Accepted

## Confidence
High

## Context
`[audit] retention_days` is parsed, validated (`>= 1`) and documented as how long
audit entries are retained before pruning, but nothing ever prunes. The
`audit_log` bucket therefore grows without bound: on a busy cluster every
authenticated S3 and native-protocol operation appends an entry, and the metadata
DB never reclaims them. That inflates `momo.db`, slows cursor scans over the log,
and silently breaks the retention promise the configuration makes.

The audit log is hash-chained for tamper evidence, so pruning cannot simply
delete rows: the first surviving entry references the hash of a deleted
predecessor and `VerifyAuditLog` would report a broken chain.

## Decision
- Audit entries older than the retention window are pruned: The store SHALL prune audit entries whose timestamp is older than the configured retention cutoff, and the daemon SHALL run the prune at startup and once per day.
- The retained window stays tamper-verifiable: Pruning SHALL record the hash of the last deleted entry as a chain anchor in the `audit_meta` bucket, and `VerifyAuditLog` SHALL seed its chain check from that anchor.
- Pruning is observable: The daemon SHALL export `momo_audit_prune_runs_total` (passes executed) and `momo_audit_pruned_entries_total` (entries removed), and log an AUDIT line for each pass that removed at least one entry.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/069-an-audit-log-that-cannot-grow-forever.md

## References
- Issue: #1307
- PR: 
- Spec: `openspec/changes/audit-log-retention/`
- Blog: docs/blog/posts/069-an-audit-log-that-cannot-grow-forever.md

