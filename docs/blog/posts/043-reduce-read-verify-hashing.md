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

We ran our first proper performance profile on the storage engine and got a
number that made no sense. Writing an object was fast. Reading it back was
miserably slow. On a machine with a fast NVMe drive, the same bytes that went in
at roughly 2400 MB/s came out at about 313 MB/s — reading was nearly eight times
slower than writing.

A read is supposed to be the easy direction. Something was badly wrong.

The profiler pointed at one function: the SHA-256 hash. Nearly all CPU cycles
were going into recomputing a cryptographic digest of files we had already
verified minutes earlier. To protect against silent disk corruption, our
**verify-on-read** logic re-hashed every object on every single read, even when
nothing had changed. The storage server was CPU-bound, spending its time
re-proving something it already knew.

## The Real Problem

Verify-on-read exists for a good reason. **Bitrot** — silent, localized disk
corruption — can flip bits in a file without the disk reporting an error. The
defense is to hash the object as you read it and compare against the known-good
digest; a mismatch means the bytes are bad, and you return an error instead of
serving corruption. Hashing a large object is expensive, but it is the difference
between integrity and silent data loss.

The trouble is that we had applied the defense uniformly, with no memory. Every
read paid the full cost, even the hundredth read of the same immutable file in
the same process. In a read-heavy workload, nearly every request re-derived a
digest whose answer could not have changed since the last time we computed it.

## Why the Obvious Solutions Failed

**"Add a toggle to disable verify-on-read."** This is the tempting fix and
exactly the wrong one. Turning verification off converts silent bitrot into
silent data corruption — the worst outcome in a storage system. Any solution had
to keep integrity intact.

**"Cache the digest on disk."** Persisting trust markers across restarts is
worse than not having them: a marker written before the disk rotted would
decline to re-check after the rot, and a restart would carry stale trust forward.
Trust must not survive a process restart.

**"Only verify small objects."** Size is not the risk. A corrupted 4 KB block can
hide in a huge file; skipping verification for large objects is backwards.

So we set three non-negotiable invariants:

1. **Zero compromise on cold reads.** The first time a blob is read in a server's
   life, it is fully verified.
2. **Zero persisted trust.** Trust markers live only in memory and vanish on
   restart, forcing a fresh verification pass.
3. **Continuous background detection.** Independent scrubbers keep auditing disk
   blocks regardless of read traffic.

## The Solution: Dynamically Earned Trust

We introduced a **seam** — a compile-time interface marking the one place the
storage engine is allowed to hook into verification — called `ReadVerifier`:

```go
type ReadVerifier interface {
    // Verify returns an io.ReadCloser that verifies hash at EOF,
    // or returns the underlying reader directly if already trusted.
    Verify(underlying io.ReadCloser, hash string) io.ReadCloser

    // MarkTrusted records that this blob has passed a full SHA-256 verification.
    MarkTrusted(hash string)
}
```

Two implementations ship compiled in:

1. **`everyReadVerifier` (default).** The historical behavior: every read is
   wrapped in a verifying reader that hashes to end-of-file.
2. **`verifiedCache` (opt-in).** Remembers, per process, which blobs have already
   passed a full verification, and skips re-hashing them:

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

The key word is *earned*. A blob is trusted only after it has actually been
verified end to end in this process. The first read pays the full hash cost; every
subsequent warm read streams bytes straight to the socket with no hashing at all.

The read path branches on that trust cache:

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

## The Trade-off: Speed vs. the Bitrot Window

| Dimension | `everyReadVerifier` | `verifiedCache` |
|---|---|---|
| **Cold Read Throughput** | ~313 MB/s (CPU-bound) | ~313 MB/s (CPU-bound) |
| **Warm Read Throughput** | ~313 MB/s (CPU-bound) | **~2400 MB/s (disk-bound, 7.6x faster)** |
| **Memory Footprint** | zero bytes of state | roughly 64 bytes per unique active blob in RAM |
| **Bitrot Window** | Instant detection on every read | Detected on cold read; later rot caught by background scrub |
| **Allocations per Op** | Scales with chunk count | **Constant 11 allocations/op** (hasher omitted on warm reads) |

The honest cost is the bitrot window. Here is exactly what it means: if a disk
sector rots *after* the first read has marked the blob trusted, `verifiedCache`
will serve the bad bytes until the next background scrub pass notices. That window
is bounded — the scrubber periodically scans all stored blobs, bypassing the read
cache, and on a mismatch it purges the blob from the trust cache, quarantines it,
and triggers a self-heal rebuild from a healthy replica.

We accepted a bounded detection delay on the rare post-first-read corruption in
exchange for removing a hashing tax from every warm read. The alternative — a
permanent 8x read penalty for a defense that mostly re-proves unchanged files —
was worse.

## How We Verified

Benchmarks confirmed the improvement:

```
BenchmarkReadVerify/1MiB-4         1000   3,189,451 ns/op    313.52 MB/s     28 allocs/op
BenchmarkTrustedBlobRead/1MiB-4   10000     416,210 ns/op   2402.63 MB/s     11 allocs/op
```

- **Throughput:** warm reads went from ~313 MB/s to over 2400 MB/s.
- **Allocation invariant:** the trusted read path holds a constant 11
  allocations per operation regardless of payload size — 1 MiB, 64 MiB, or 256
  MiB — because the crypto engine is simply absent from the warm path.

See [docs/STANDARDS.md](../../STANDARDS.md) for the Bolt and Sentinel engineering
principles.

## What Could Go Wrong

- **Trust is per-process, by design.** A restart wipes the cache and every blob
  is re-verified on its first read after boot. That is desirable, and it means a
  restart temporarily loses the warm-read speedup.
- **A hash collision in the trusted set.** The set is keyed by object digest, so
  two objects with the same hash would share trust. SHA-256 makes this a
  theoretical non-issue, but the design depends on the digest being the identity.
- **Concurrency.** The trust map is guarded by a read-write mutex; writes are
  rare (once per blob per process), reads are frequent.
- **A scrubbing gap.** If the background scrubber stops running, the bitrot
  window stops being bounded. Scrub is part of the guarantee, not decoration.

## When NOT to Use This

- **Short-lived or one-shot processes.** If a process reads each blob once and
  exits, there are no warm reads to speed up and the cache only adds bookkeeping.
- **When instant detection is mandatory.** Systems that must detect corruption on
  the very read where it occurs should keep the always-verify behavior.
- **As a replacement for scrubbing.** This optimization assumes an independent
  scrubber bounds the window; remove the scrubber and you have re-introduced
  silent corruption.

## References / Dig deeper

- Storage integrity and GC: [007](007-at-rest-integrity-and-gc.md).
- Profiling baseline: [042](042-perf-profiling-baseline.md).
- Central integrity verification: [031](031-core-integrity-verification.md).
- Seams architecture: [044](044-plugin-seam-architecture.md).
- Implementation: `src/storage/read_verifier.go`; benchmarks:
  `src/storage/bench_test.go`.
- Spec: `openspec/changes/reduce-read-verify-hashing`; PR #951; issue #950.
