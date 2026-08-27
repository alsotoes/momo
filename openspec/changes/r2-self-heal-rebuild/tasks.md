# Tasks: R2 — Degraded-read + self-heal rebuild (#930)

## 1. Survivor read path (`src/storage`)
- [x] On primary read failure, attempt other placed replicas, verify bytes vs content hash
      (reuse `HashReader`/verifying reader), serve first verified survivor — R2-C1
- [x] Quarantine mark-and-hold (non-final) so rebuild can replace after re-replication —
      R2-C2
- [x] Read fails `ENOENT`/`EIO` only when no verified survivor exists

## 2. Self-heal loop (`src/storage`)
- [x] `StartRebuild`/`rebuildLoop` mirroring `StartScrub`/`StartGC` (sync.Once, cancellable,
      goleak-safe) — R2-C3
- [x] Iterate content-address entries; re-replicate blobs with verified-replica count <
      target from a verified survivor — R2-C5 (failure-domain-aware when R1 present)
- [x] Verify-before-use on all source replicas — R2-C4
- [x] Bounded worker pool, panic-recovered, cancel on Close — R2-C6

## 3. Config
- [x] `rebuild_interval` (300), `degraded_read` (true), `rebuild_workers` (4) in
      `[storage]` — R2-G1
- [x] `conf/momo.conf` + `docs/CONFIGURATION.md` (Rule 27)

## 4. Tests (`src/storage/rebuild_test.go`)
- [x] R2-T1 survivor read + mark-and-hold + replace
- [x] R2-T2 kill-one-of-3 → restored to 3 verified (R1-aware)
- [x] R2-T3 no corrupt propagation
- [x] R2-T4 goleak + `-race`, Close stops loop

## 5. Validation
- [x] `go fmt`, `go vet`, `go build`, `go test` (storage)
- [x] `go work sync` + vendor parity
- [x] Docs: `docs/ARCHITECTURE.md` §4f + integity/scrub docs updated
