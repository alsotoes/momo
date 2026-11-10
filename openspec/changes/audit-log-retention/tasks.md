# Tasks — Enforce Audit Log Retention

## Phase 1 — Storage
- [x] Add `bucketAuditMeta` and the `pruned_through` anchor key.
- [x] Add `CASStore.PruneAuditLog(cutoff time.Time) (int, error)` deleting older
      entries in one write transaction and recording the last deleted hash.
- [x] Seed `VerifyAuditLog`'s chain check from the recorded anchor.
- [x] Tests: partial prune, all pruned, no-op paths (zero/past cutoff), anchor
      recorded, post-prune append still verifies, tamper detection after prune.

## Phase 2 — Daemon
- [x] `src/server/audit_retention.go`: `startAuditPruneLoop` — startup pass then
      daily ticker, context-cancelled, panic-guarded, nil-safe.
- [x] Wire from `Daemon` when the store is a `*storage.CASStore` and
      `retention_days > 0`.
- [x] Tests: nil store and disabled retention exit immediately; startup pass runs
      once and the loop stops on cancel.

## Phase 3 — Observability
- [x] `momo_audit_prune_runs_total`, `momo_audit_pruned_entries_total`.
- [x] AUDIT log line per non-empty prune (count + window); failures logged while
      the run counter still advances.

## Phase 4 — Docs & Compliance
- [x] `docs/GUIDES/CONFIGURATION.md` documents the enforced retention and the
      tamper-evidence trade-off.
- [x] `docs/ARCHITECTURE.md` describes the prune loop and the chain anchor.
- [x] Blog post (Rule 76).
- [x] ADR via `make adr-sync` (Rules 77/78).

## Cross-Phase Requirements
- [x] `go build ./...`, `go test ./...`, `go vet`, `gofmt -l` clean.
- [x] `make lint` clean (complexity budget, Rule 344).
- [x] `go work vendor` parity (Rule 25).
- [x] `make blog-check`, `make diagram-check`, `make adr-sync-check` pass.
