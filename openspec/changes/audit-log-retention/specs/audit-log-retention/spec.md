> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1307

# audit-log-retention Specification

## Purpose

Keep the tamper-evident audit log bounded by the operator's configured retention
window, without losing the ability to verify the integrity of the entries that
remain.

## ADDED Requirements

### Requirement: Audit entries older than the retention window are pruned

The store SHALL prune audit entries whose timestamp is older than the configured
retention cutoff, and the daemon SHALL run the prune at startup and once per day.

#### Scenario: partial prune
- **WHEN** entries exist on both sides of the cutoff
- **THEN** the older entries are deleted, the newer ones are retained, and the
  number deleted is returned.

#### Scenario: retention disabled
- **WHEN** the cutoff is zero, or the store is not a CAS store, or
  `retention_days < 1`
- **THEN** no pruning runs and no error is returned.

#### Scenario: all entries expire
- **WHEN** every entry is older than the cutoff
- **THEN** the log becomes empty and `VerifyAuditLog` returns nil.

### Requirement: The retained window stays tamper-verifiable

Pruning SHALL record the hash of the last deleted entry as a chain anchor in the
`audit_meta` bucket, and `VerifyAuditLog` SHALL seed its chain check from that
anchor.

#### Scenario: verification after prune
- **WHEN** `VerifyAuditLog` runs over a pruned log
- **THEN** it succeeds, because the first retained entry's `prev_hash` matches the
  recorded anchor.

#### Scenario: tampering inside the retained window
- **WHEN** a retained entry's content is modified without recomputing its hash
- **THEN** `VerifyAuditLog` fails.

#### Scenario: writes after pruning
- **WHEN** a new entry is appended after a prune
- **THEN** it chains onto the last retained entry and the whole log still
  verifies.

### Requirement: Pruning is observable

The daemon SHALL export `momo_audit_prune_runs_total` (passes executed) and
`momo_audit_pruned_entries_total` (entries removed), and log an AUDIT line for
each pass that removed at least one entry.

#### Scenario: non-empty prune is recorded
- **WHEN** a pass deletes entries
- **THEN** both counters advance and the log line states the count and window.

#### Scenario: failed prune is reported
- **WHEN** the store returns an error
- **THEN** the failure is logged and the run counter still advances, so a
  persistently failing prune is visible in metrics.
