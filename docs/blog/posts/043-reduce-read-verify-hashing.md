---
title: "Reducing Read-Verify Hashing: Trust Earned by Real Verification"
date: 2026-08-26T19:00:59Z
draft: false
post_type: issue
tags: [performance, integrity, storage, bolt, sentinel]
categories: [performance, storage]
summary: "Verify-on-read re-hashes the full object on every local read (~8x slower than write). A ReadVerifier seam skips re-verification for blobs verified this process, while scrub still catches bitrot."
artifacts:
  - {type: spec, path: openspec/changes/reduce-read-verify-hashing}
  - {type: issue, id: "950"}
  - {type: pr, id: "951"}
related:
  - 007-at-rest-integrity-and-gc
  - 042-perf-profiling-baseline
  - 031-core-integrity-verification
  - 044-plugin-seam-architecture
---

During the performance profiling baseline established in [042](042-perf-profiling-baseline.md), the engineering team uncovered an alarming asymmetry in momo's storage engine: **reading an object was nearly 8× slower than writing it**.

On modern NVMe storage, momo wrote objects at ~2400 MB/s. But when serving reads with `VerifyOnRead = true`, throughput collapsed to ~313 MB/s. Profiling with Go's `pprof` revealed that nearly 98% of CPU cycles were spent inside `crypto/sha256.blockAVX2`. The storage server was completely CPU-bound, repeatedly re-computing the full SHA-256 digest of immutable local files that had already been verified minutes earlier.

This post explains how momo resolved this bottleneck using a **Rule 74 compile-time seam** (`ReadVerifier`) and the principle of **dynamically earned trust**.

```
========================================================================================
                          DYNAMICALLY EARNED TRUST READ PATH
========================================================================================

 Client GET /file.bin
         |
         v
 [ CASStore.Get() ]
         |
         v
 Check Trust Cache: IsTrusted(hash)?
         |
         +---------------------------------------+
         |                                       |
     [ NO: First Read / Cold ]               [ YES: Previously Verified ]
         |                                       |
         v                                       v
 [ verifyingReader ]                     [ Raw LocalBlobStore Stream ]
 - Stream bytes to client                - Stream bytes directly to socket
 - Stream bytes to SHA-256               - Zero hashing CPU overhead
         |                                       |
         v                                       v
 At io.EOF: Digest Matches?              Max NVMe Read Throughput (~2400 MB/s)
   |
   +---> [ YES ] -> MarkTrusted(hash)
   |
   `---> [ NO  ] -> Return syscall.EBADMSG
```

---

## 1. The Core Dilemma: Performance vs. Data Integrity

Faced with an 8× performance penalty, the naive solution would be simple: add a config toggle to disable `VerifyOnRead`.

Under momo's **🛡 Sentinel mindset**, this was categorically rejected. Disabling verification turns silent bitrot into silent data corruption. An acceptable systems design had to satisfy three non-negotiable invariants:
1. **Zero Compromise on Cold Reads**: Any blob read from disk for the first time in a server lifecycle must be fully validated.
2. **Zero Persisted Trust**: Trust markers must never be persisted to disk. If a server restarts, all trust is erased, forcing a fresh verification pass.
3. **Continuous Background Bitrot Detection**: Background scrubbers ([007](007-at-rest-integrity-and-gc.md)) must continue auditing disk blocks independently of read traffic.

---

## 2. The Implementation: `ReadVerifier` Seam

In `src/storage/read_verifier.go`, momo introduced a compile-time interface seam following **Rule 74 (Seam-Over-Plugins)**:

```go
type ReadVerifier interface {
    // Verify returns an io.ReadCloser that verifies hash at EOF,
    // or returns the underlying reader directly if already trusted.
    Verify(underlying io.ReadCloser, hash string) io.ReadCloser

    // MarkTrusted records that this blob has passed a full SHA-256 verification.
    MarkTrusted(hash string)
}
```

The engine ships two compiled-in implementations:

1. **`everyReadVerifier` (Default)**: Preserves the historical behavior—always wraps every read in `verifyingReader`.
2. **`verifiedCache` (Opt-in)**: Skips re-hashing once a blob has completed a successful verification pass in the current process lifecycle:

```go
type verifiedCache struct {
    mu      sync.RWMutex
    trusted map[string]struct{}
}

func (v *verifiedCache) Verify(underlying io.ReadCloser, hash string) io.ReadCloser {
    v.mu.RLock()
    _, ok := v.trusted[hash]
    v.mu.RUnlock()

    if ok {
        // Blob was already verified during this process lifecycle.
        // Return raw stream directly — 0 hashing overhead!
        return underlying
    }

    // Wrap in verifyingReader with callback to record trust on clean EOF
    vr := newVerifyingReader(underlying, hash)
    vr.onVerified = func() {
        v.MarkTrusted(hash)
    }
    return &verifyingReadCloser{verifyingReader: vr, underlying: underlying}
}

func (v *verifiedCache) MarkTrusted(hash string) {
    v.mu.Lock()
    v.trusted[hash] = struct{}{}
    v.mu.Unlock()
}
```

---

## 3. Tradeoff Analysis & Memory Footprint

| Dimension | `everyReadVerifier` | `verifiedCache` |
|---|---|---|
| **Cold Read Throughput** | ~313 MB/s (CPU-bound) | ~313 MB/s (CPU-bound) |
| **Warm Read Throughput** | ~313 MB/s (CPU-bound) | **~2400 MB/s (Disk/Bus-bound, 7.6× faster)** |
| **Memory Footprint** | $0\text{ bytes}$ state | $\sim 64\text{ bytes}$ per unique active blob in RAM |
| **Bitrot Window** | Instant detection | Detected on cold read; subsequent rot caught by background scrub |
| **Allocations per Op** | Scales with chunk count | **Constant 11 allocs/op** (hasher omitted on warm reads) |

### What if a bit flips after the first read?
If a disk sector rots *after* the initial read has marked it trusted, does `verifiedCache` serve bad data? Yes, until the next background scrub cycle. However, this window is strictly bounded: `StartScrub` periodically scans all stored blobs, bypassing the read-cache. If a mismatch is discovered during a scrub pass, `CASStore` purges the blob from the trusted cache, quarantines it, and triggers a self-heal rebuild from replicas.

---

## 4. Empirical Verification & Benchmarks

The benchmark suite in `src/storage/bench_test.go` confirmed dramatic efficiency improvements:

```
BenchmarkReadVerify/1MiB-4         1000   3,189,451 ns/op    313.52 MB/s     28 allocs/op
BenchmarkTrustedBlobRead/1MiB-4   10000     416,210 ns/op   2402.63 MB/s     11 allocs/op
```

- **Throughput Jump**: Throughput surged from **313 MB/s to over 2400 MB/s** on repeated reads.
- **Allocation Invariant**: `BenchmarkTrustedBlobRead` maintains a **constant 11 allocations/op** regardless of payload size (1 MiB, 64 MiB, or 256 MiB), as the crypto engine is completely removed from the warm read path.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt and 🛡 Sentinel engineering principles.

## Related

- Storage integrity & GC: [007](007-at-rest-integrity-and-gc.md)
- Profiling baseline: [042](042-perf-profiling-baseline.md)
- Central integrity verification: [031](031-core-integrity-verification.md)
- Seams architecture (Rule 74): [044](044-plugin-seam-architecture.md)
