---
title: "Reducing Read-Verify Hashing: Trust Earned by Real Verification"
date: 2026-08-26T19:00:59Z
draft: false
tags: [performance, integrity, storage, bolt, sentinel]
categories: [performance, storage]
summary: "Verify-on-read re-hashes the full object on every local read (~8x slower than write). A ReadVerifier seam skips re-verification for blobs verified this process, while scrub still catches bitrot."
artifacts:
  - {type: spec, path: openspec/changes/reduce-read-verify-hashing}
  - {type: issue, id: "950"}
related:
  - 007-at-rest-integrity-and-gc
  - 042-perf-profiling-baseline
  - 031-core-integrity-verification
  - 044-plugin-seam-architecture
---
The baseline in [042](042-perf-profiling-baseline.md) exposed the biggest hot
path: **verify-on-read re-hashes the entire object on every local read** — ~8×
slower per byte than writing it. The fix isn't to drop integrity; it's to stop
re-verifying data this process has already verified.

## The measurement

| Benchmark | Throughput |
|-----------|-----------|
| LocalWrite | ~2400 MB/s |
| ReadVerify | ~313 MB/s |

Hashing dominates reads. Every `Get` re-ran SHA-256 over the full blob even
when the same process had just verified that exact blob on write.

## The `ReadVerifier` seam (Rule 74)

`src/storage/read_verifier.go` introduces a compile-time seam — the project's
"trust core, seam the changeable" pattern:

```go
type ReadVerifier interface {
    Verify(r io.Reader) error      // full verification
    MarkTrusted(name string) bool  // record verified-trust
    IsTrusted(name string) bool    // skip if already verified
}
```

- **`everyReadVerifier`** — default: always verify (today's behavior, unchanged)
- **`verifiedCache`** — opt-in: skip re-verification for blobs verified this
  process

**Trust is only earned by a real verification** — a blob enters the trusted set
only after an actual verify passes (read EOF or scrub match). The trusted set
is empty on boot: the first read of a blob verifies it, subsequent reads in the
same process skip. Nothing is persisted — a restart re-verifies once.

## Why this is safe

- **Verify still runs**: on write, on first read per process, and on every scrub
- **Scrub re-catches bitrot**: `StartScrub` still reads and verifies every blob
  on its interval — corruption that happens after boot is still found
- **No persisted trust**: a restart forgets trust, so cross-process staleness
  can't sneak in
- **Backward compatible**: `VerifyOnRead` keeps its contract; default behavior
  is unchanged

## Result

`TrustedRead` holds **constant 11 allocations** regardless of blob size (the
hasher leaves the hot loop) versus `ReadVerify` growing with size. Cold path
regression: none.

## ⚡ Bolt / 🛡 Sentinel lens

⚡ **Bolt**: 8× hot-path win, constant allocation. 🛡 **Sentinel**: the trust
model is *earned, not assumed* — this is not "disable integrity," it's
"don't redo the work you already did." Bitrot detection is preserved via scrub.

See [docs/STANDARDS.md](../../STANDARDS.md).

## Related

Integrity: [007](007-at-rest-integrity-and-gc.md), [031](031-core-integrity-verification.md).
Baseline: [042](042-perf-profiling-baseline.md).
