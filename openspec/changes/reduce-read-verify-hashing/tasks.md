# Tasks: reduce-read-verify-hashing — immutable-CAS verified-cache read path (#950)

## 1. ReadVerifier seam (`src/storage`)
- [ ] Define `ReadVerifier` interface + `everyReadVerifier` + `verifiedCache`
      impls (trusted map, mutex-guarded), compiled-in registry (RV-T1)
- [ ] Replace `if s.VerifyOnRead { ... }` boolean gate at read wrap-point
      (`storage.go`, ~line 316) with verifier dispatch; keep `VerifyOnRead bool`
      backward-compatible knob → `everyReadVerifier` on / `verifiedCache` off
      (RV-T2)
- [ ] Constructor functional option on `newCASStore` to select verifier;
      default = current behavior (RV-T3)

## 2. Trust bookkeeping (scrub + read)
- [ ] Mark trusted on successful read-path EOF verify (RV-T4)
- [ ] Mark trusted on `scrubBlob` digest match (`integrity.go:195`) (RV-T5)
- [ ] Trusted set empty on boot (`newCASStore`); in-process only, no schema
      change (RV-T6)

## 3. Integrity soundness tests
- [ ] First-read verifies + establishes trust; second read skips (RV-T7)
- [ ] Scrub establishes trust (RV-T8)
- [ ] No trust without prior in-process verification (RV-T9)
- [ ] Corruption still caught/quarantined by scrub for trusted blobs (RV-T10)
- [ ] Default behavior unchanged vs `VerifyOnRead` on/off (RV-T11)

## 4. Benchmark gate (Rule 73)
- [ ] Repeated-read-of-trusted-blob bench approaches raw disk read ~2400 MB/s,
      no `BenchmarkReadVerify` cold-path regression (RV-T12)

## 5. OpenSpec set (Rule 11 / Rule 73)
- [x] Author `openspec/changes/reduce-read-verify-hashing/{proposal,spec,tasks}`
      linked to issue #950 (RV-T13)

## 6. Validation
- [ ] `go vet` + `go test -race -cover` green for `src/storage` (RV-T14)
- [ ] `git diff master...HEAD --name-only` = storage seam/tests + OpenSpec set
      only (RV-T15)
- [ ] CI green incl. `review` (Rule 13)
