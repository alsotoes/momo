# Tasks: reduce-read-verify-hashing — immutable-CAS verified-cache read path (#950)

## 1. ReadVerifier seam (`src/storage`)
- [x] Define `ReadVerifier` interface + `everyReadVerifier` + `verifiedCache`
      impls (trusted map, mutex-guarded), compiled-in registry (RV-T1)
- [x] Replace `if s.VerifyOnRead { ... }` boolean gate at read wrap-point
      (`storage.go`, ~line 316) with verifier dispatch; keep `VerifyOnRead bool`
      backward-compatible knob → `everyReadVerifier` on / `verifiedCache` off
      (RV-T2)
- [x] Constructor functional option on `newCASStore` to select verifier;
      default = current behavior (RV-T3)

## 2. Trust bookkeeping (scrub + read)
- [x] Mark trusted on successful read-path EOF verify (RV-T4)
- [x] Mark trusted on `scrubBlob` digest match (`integrity.go:195`) (RV-T5)
- [x] Trusted set empty on boot (`newCASStore`); in-process only, no schema
      change (RV-T6)

## 3. Integrity soundness tests
- [x] First-read verifies + establishes trust; second read skips (RV-T7)
- [x] Scrub establishes trust (RV-T8)
- [x] No trust without prior in-process verification (RV-T9)
- [x] Corruption still caught/quarantined by scrub for trusted blobs (RV-T10)
- [x] Default behavior unchanged vs `VerifyOnRead` on/off (RV-T11)

## 4. Benchmark gate (Rule 73)
- [x] Repeated-read-of-trusted-blob bench approaches raw disk read ~2400 MB/s,
      no `BenchmarkReadVerify` cold-path regression (RV-T12)

## 5. OpenSpec set (Rule 11 / Rule 73)
- [x] Author `openspec/changes/reduce-read-verify-hashing/{proposal,spec,tasks}`
      linked to issue #950 (RV-T13)

## 6. Validation
- [x] `go vet` + `go test -race -cover` green for `src/storage` (RV-T14)
- [x] `git diff master...HEAD --name-only` = storage seam/tests + OpenSpec set
      only (RV-T15)
- [x] CI green incl. `review` (Rule 13)
