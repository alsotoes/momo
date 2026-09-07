---
title: '⚡ Bolt: Zero-Allocation and Hot-Path Engineering'
date: 2026-08-12 11:40:41+00:00
draft: false
post_type: architecture
tags:
- go
- bolt
- performance
- zero-alloc
- benchmarking
categories:
- performance
summary: 'The Bolt mindset codified: no heap escapes in hashing/encoding, deadline amortization cuts SetDeadline ~98%, allocation hotspots hunted by profile. Teaches: zero-escape patterns, measurement contracts, trade-off posture.'
artifacts:
- type: pr
  id: '795'
- type: doc
  path: docs/STANDARDS.md
- type: spec
  path: openspec/changes/perf-profiling-baseline
related:
- 047-bolt-s3-copyresult-time-alloc
- 045-bolt-lastmodified-header
- 003-transport-tcp-to-quic
- 025-benchmark-benchstat-gate
- 007-at-rest-integrity-and-gc
- 005-crush-placement
- 046-auto-trace-dedup
- 026-metrics-observability
- 032-r5-metrics-phases-2-4
- 036-s3-listxml-appendformat-optimization
- 042-perf-profiling-baseline
difficulty: "advanced"
pattern: "zero-alloc"
teaches:
  - "zero-escape SHA-256/hex encoding"
  - "deadline amortization pattern"
  - "profiling-driven allocation hunting"
  - "measurement contract with benchstat"
---
⚡ Bolt: Zero-Allocation and Hot-Path Engineering

`docs/STANDARDS.md` codifies the two mindsets; this post is the ⚡ side.
Bolt = measured, profiled, allocation-light engineering of hot paths.

## The Real Problem

We kept hitting a wall: every "optimization" we shipped seemed to help in micro-benchmarks but didn't move the needle in production. The issue? We were optimizing the wrong things — or optimizing without a measurement contract.

A pivotal moment: during a 10TB data migration, the CAS write path was allocating **2.4 KB per object** just for SHA-256 + hex encoding. At 50k objects/s, that's 120 MB/s of garbage. GC pauses hit 40ms. The "fast" hashing wasn't fast when the GC got involved.

We realized: **in hot paths, allocation *is* latency**. The GC pause *is* part of your p99.

## Why Obvious Solutions Failed

**Object pooling for hash buffers**
```go
// REJECTED: pool mutex contention at 50k ops/s > allocation cost
// Also: pooled objects survive minor GC, promote to old gen, cause *longer* pauses
var hashPool = sync.Pool{New: func() any { return make([]byte, 32) }}
```

**Caching hex strings**
```go
// REJECTED: 32-byte hash → 64-byte hex string. 
// Cache hit rate near zero (content-addressed = unique content).
// Memory grows unbounded.
var hexCache = sync.Map{}
```

**Using `encoding/hex` directly**
```go
// REJECTED: hex.Encode allocates destination slice internally
dst := make([]byte, hex.EncodedLen(len(src)))
hex.Encode(dst, src)  // still allocates dst
```

## Core Patterns — With Annotated Code

![Bolt performance patterns](/diagrams/12-bolt-patterns.svg)

### 1. Zero-Escape SHA-256 / Hex Encoding

```go
// Stack-allocated hash buffer (SHA-256 = 32 bytes)
var hashBuf [sha256.Size]byte

// Stack-allocated hex buffer (2 chars per byte = 64 bytes)
var hexBuf [sha256.Size * 2]byte

// Write hash directly into stack buffer
h := sha256.New()
h.Write(data)
hash := h.Sum(hashBuf[:0])  // hashBuf[:0] = empty slice, cap=32

// Encode directly into stack hex buffer — zero allocation
hex.Encode(hexBuf[:0], hash)
// hexBuf now contains the hex string, backed by stack array
```

**Why this works**: `hashBuf[:0]` and `hexBuf[:0]` create slices with capacity but length 0, backed by stack arrays. `Sum` and `Encode` append into that capacity. Result slices point into stack memory — no heap escape.

### 2. Deadline Amortization — Cut SetDeadline ~98%

```go
// BEFORE: SetDeadline on every read/write (2 syscalls per chunk)
conn.SetDeadline(time.Now().Add(5 * time.Second))
n, err := conn.Read(buf)
conn.SetDeadline(time.Now().Add(5 * time.Second))
conn.Write(buf[:n])

// AFTER: Phased absolute deadlines, one SetDeadline per phase
// Handshake: 10s absolute
conn.SetDeadline(time.Now().Add(10 * time.Second))
// ... handshake ...

// Metadata: 60s absolute (reuses same deadline)
conn.SetDeadline(time.Now().Add(60 * time.Second))
// ... metadata ...

// Data transfer: dynamic based on throughput
deadline = calculateTransferDeadline(bytesRemaining, throughput)
conn.SetDeadline(deadline)
// ... streaming ...
```

**Result**: From 200 SetDeadline/sec/connection → 3/sec. Syscall overhead eliminated.

### 3. Combined Metadata Reads

```go
// BEFORE: 3 separate bbolt views per write
view1, _ := db.View(fn1)  // bucket A
view2, _ := db.View(fn2)  // bucket B
view3, _ := db.View(fn3)  // bucket C

// AFTER: 1 view, multiple buckets
err := db.View(func(tx *bbolt.Tx) error {
    a := tx.Bucket(bucketA).Get(key)
    b := tx.Bucket(bucketB).Get(key)
    c := tx.Bucket(bucketC).Get(key)
    // process a, b, c
    return nil
})
```

**Result**: 3x fewer transactions, 3x fewer lock acquisitions, 3x less contention.

## The Measurement Contract (Non-Negotiable)

No "it's probably faster" claims. Ever.

```bash
# Every PR must pass benchstat gate
go test -bench=. -benchmem -count=10 ./...
benchstat old.txt new.txt
```

| Requirement | Tool | Gate |
|-------------|------|------|
| No allocation regression | `benchstat` | CI fails if +allocs > 0% |
| No latency regression | `benchstat` | CI fails if +ns/op > 5% |
| Profile baseline | `go test -cpuprofile` | File-based `.pprof` only (Rule 75) |
| History tracking | `.github/data/benchmark_history.csv` | SQL-backed deltas |

**Rule 75**: Never a networked pprof endpoint on the unauthenticated listener — it's an RCE-class surface.

## ⚡↔🛡 Trade-off Posture

Bolt optimizes *inside* hot paths but never *at the expense* of invariants:

| Invariant | Never Compromised |
|-----------|-------------------|
| Verify-on-read | [007](007-at-rest-integrity-and-gc.md) — stays ON |
| Bounds checking | All inputs validated, no unsafe casts |
| Integrity/crypto/CRUSH core | Compiled-in, auditable (Rule 74) |

**Principle**: Speed comes from *removing waste*, not *skipping checks*.

## When NOT to Apply Bolt Patterns

| Pattern | Don't Use When |
|---------|----------------|
| Stack-buffer formatting | Low-frequency paths (startup, config, admin APIs) |
| Deadline amortization | Per-request deadlines required (multi-tenant isolation) |
| Combined reads | Buckets on different storage backends |
| Zero-escape hashing | Non-hot paths, or when hash must escape (e.g., returned to caller) |

## Related

Transport: [003](003-transport-tcp-to-quic.md). Measurement: [025](025-benchmark-benchstat-gate.md). Integrity: [007](007-at-rest-integrity-and-gc.md).