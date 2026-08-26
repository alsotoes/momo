> GitHub Issue URL: https://github.com/alsotoes/momo/issues/950

# reduce-read-verify-hashing Specification

## Purpose
Eliminate redundant full-object SHA-256 re-hashing on reads of blobs already
proven good, exploiting content-address immutability, via a policy-selected
`ReadVerifier` seam (Rule 74). Integrity is preserved: trust is only ever
granted after a real successful full verification, and background scrub keeps
catching silent disk rot. Default behavior is unchanged.

## Requirements

### Requirement: ReadVerifier seam
A `ReadVerifier` MUST replace the raw boolean `VerifyOnRead` gate at the
read wrap-point (`storage.go` around the `if s.VerifyOnRead { ... }` block that
wraps `GetBlob` output in `verifyingReadCloser`) with a policy-chosen verifier.

#### Scenario: two compiled-in implementations
- `everyReadVerifier.Verify(hash)` MUST return true always (current behavior).
- `verifiedCache` MUST verify only blobs not present in its trusted set.

#### Scenario: default preserves current behavior
With no policy override, read behavior MUST be identical to today
(`VerifyOnRead` on → full verify; off → no verify). Functional-option /
constructor mapping MUST keep the exported `VerifyOnRead bool` field working for
existing callers and tests (backward compatible).

### Requirement: trust is earned, immutable
A blob MAY be added to the verified set ONLY as a consequence of a successful
full SHA-256 verification of its bytes against its content-address key, from
either source: read-path streaming verify at EOF, or a `scrubBlob` digest match
(`integrity.go:195`).

#### Scenario: first-read establishes trust
A read of a never-verified blob performs the full verify; on success the blob is
marked trusted. The NEXT read of that same blob (no intervening mutation — blobs
are immutable) MUST skip re-hashing.

#### Scenario: scrub establishes trust
A blob whose `scrubBlob` returns digest==hash (no quarantine) is marked trusted
and subsequent reads skip re-hashing.

#### Scenario: no trust without verification
A blob read when the store is configured to never verify (or before any success)
MUST NOT be (re)added to the trusted set. The trusted set starts empty at process
start (fresh `newCASStore`), so every boot re-verifies each blob at least once.

### Requirement: integrity preservation
Skipping read re-hash MUST NOT reduce scrub coverage and MUST NOT ever trust a
blob that was not fully verified against its content-address in process.

#### Scenario: corruption is still caught
If a trusted blob's on-disk bytes rot after verification (impossible for the
immutable logical object, but possible for the physical store), the background
scrub MUST still detect and quarantine it on its periodic pass.

#### Scenario: no mutation race
Because content-addresses are immutable and verified blobs are never rewritten
to a different hash for the same key, trust established once stays valid; no
write can change a trusted blob's bytes for its verified address.

### Requirement: benchmark gate (Rule 73)
Performance MUST be benchmark-proven and must not regress the cold path.

#### Scenario: repeated-read speedup
A benchmark reading an ALREADY-TRUSTED blob repeatedly MUST show a move toward
raw disk-read cost (baseline `BenchmarkLocalWrite/1MiB` ~2400 MB/s) versus the
current cold `BenchmarkReadVerify/1MiB` ~313 MB/s.

#### Scenario: no cold-path regression
`BenchmarkReadVerify` (first/no-trust read, full verify) MUST NOT regress.

## Success Criteria

- `ReadVerifier` seam + two impls in `src/storage`; default = current behavior.
- Trusted-set mutex-guarded, in-process, empty on boot.
- `make test` green; new/UPDATED benchmark showing trusted-path speedup and
  cold-path non-regression.
- PR ships `Resolves #950`; three-dot diff scoped to seam + tests + this set.
