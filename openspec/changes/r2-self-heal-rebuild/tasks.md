# Tasks: R2 — Degraded-read + self-heal rebuild (#930)

## 1. Survivor read path (`src/storage`)
- [ ] On primary read failure, attempt other placed replicas, verify bytes vs content hash
      (reuse `HashReader`/verifying reader), serve first verified survivor — R2-C1
- [ ] Quarantine mark-and-hold (non-final) so rebuild can replace after re-replication —
      R2-C2
- [ ] Read fails `ENOENT`/`EIO` only when no verified survivor exists

## 2. Self-heal loop (`src/storage`)
- [ ] `StartRebuild`/`rebuildLoop` mirroring `StartScrub`/`StartGC` (sync.Once, cancellable,
      goleak-safe) — R2-C3
- [ ] Iterate content-address entries; re-replicate blobs with verified-replica count <
      target from a verified survivor — R2-C5 (failure-domain-aware when R1 present)
- [ ] Verify-before-use on all source replicas — R2-C4
- [ ] Bounded worker pool, panic-recovered, cancel on Close — R2-C6

## 3. Config
- [ ] `rebuild_interval` (300), `degraded_read` (true), `rebuild_workers` (4) in
      `[storage]` — R2-G1
- [ ] `conf/momo.conf` + `docs/CONFIGURATION.md` (Rule 27)

## 4. Tests (`src/storage/rebuild_test.go`)
- [ ] R2-T1 survivor read + mark-and-hold + replace
- [ ] R2-T2 kill-one-of-3 → restored to 3 verified (R1-aware)
- [ ] R2-T3 no corrupt propagation
- [ ] R2-T4 goleak + `-race`, Close stops loop

## 5. Validation
- [ ] `go fmt`, `go vet`, `go build`, `go test` (storage)
- [ ] `go work sync` + vendor parity
- [ ] Docs: `docs/ARCHITECTURE.md` §4f + integity/scrub docs updated
