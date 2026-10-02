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
- 049-eliminate-redundant-time-format-allocations
- 048-bolt-listparts-appendformat
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

Every "optimization" we shipped seemed to win in micro-benchmarks and then do
nothing in production. We were tuning the wrong things — and worse, we had no
way to tell which things were wrong. The fix was not another clever trick; it
was a discipline, and the discipline earned its name the hard way.

The moment it clicked was a 10 TB data migration. The content-addressed write
path was allocating 2.4 KB per object just to hash the object's contents and
render that hash as hexadecimal text. At 50k objects per second that is 120 MB/s
of garbage, and the garbage collector's pauses reached 40 ms — an eternity next
to a latency target in the low milliseconds. The "fast" hashing was not fast
once the collector got involved.

The mental model is one sentence: **in a hot path, allocation is latency.** A
garbage-collection pause is part of your p99 whether or not you wrote it down.

A few terms, because the rest of the post leans on them. An **allocation** is
memory taken from the heap, the shared pool the runtime manages. A **heap
escape** is when a value that could have stayed on the function's stack is
pushed onto the heap because something outlives the call. **Garbage collection
(GC)** is the runtime reclaiming that heap memory, and its pauses are time your
request spends waiting. A **hot path** is code that runs on nearly every
request. A **syscall** is a call from the program into the operating-system
kernel; each one pays a context-switch cost.

`docs/STANDARDS.md` codifies the two engineering mindsets; this post is the
⚡ Bolt side — measured, profiled, allocation-light work on hot paths.

## Why the Obvious Solutions Failed

**Pool the hash buffers.** Reusing a buffer through a pool sounds free, but the
pool's mutex becomes a contention point at 50k operations per second — more
expensive than the allocation it removes. Worse, pooled objects survive the
young generation of the collector and get promoted to the old generation, which
produces *longer* pauses, not shorter ones.

```go
// REJECTED: pool mutex contention at 50k ops/s costs more than the allocation,
// and promoted pooled objects make pauses longer.
var hashPool = sync.Pool{New: func() any { return make([]byte, 32) }}
```

**Cache the hex strings.** Content-addressed storage means every object's hash
is effectively unique, so a cache would have a near-zero hit rate while growing
without bound.

```go
// REJECTED: content-addressed hashes are unique → near-zero hit rate, unbounded memory.
var hexCache = sync.Map{}
```

**Call the standard encoder and accept its destination buffer.** The standard
hex encoder writes into a destination slice you have to allocate first, so the
allocation just moves rather than disappearing.

```go
// REJECTED: hex.Encode writes into dst, but dst itself still has to be allocated.
dst := make([]byte, hex.EncodedLen(len(src)))
hex.Encode(dst, src)
```

## The Solution — Three Patterns With Annotated Code

{{< diagram src="/diagrams/12-bolt-patterns.svg" alt="Bolt performance patterns overview" caption="Figure 1: The three core pillars of Bolt performance engineering in momo" >}}

### 1. Zero-Escape SHA-256 and Hex Encoding

{{< diagram src="/diagrams/12a-zero-escape.svg" alt="Zero-escape stack buffers" caption="Figure 2: Stack arrays vs heap pooling — eliminating garbage collector pauses" >}}

A stack array lives and dies with the function call. If we hand the hashing and
encoding routines a slice backed by that array, their output never touches the
heap.

```go
// Stack-allocated hash buffer (SHA-256 = 32 bytes)
var hashBuf [sha256.Size]byte

// Stack-allocated hex buffer (2 chars per byte = 64 bytes)
var hexBuf [sha256.Size * 2]byte

// Write the hash directly into the stack buffer
h := sha256.New()
h.Write(data)
hash := h.Sum(hashBuf[:0])  // hashBuf[:0] = empty slice, cap=32

// Encode directly into the stack hex buffer — zero allocation
hex.Encode(hexBuf[:0], hash)
// hexBuf now holds the hex text, backed by the stack array
```

`hashBuf[:0]` and `hexBuf[:0]` are the trick: each creates a slice with the
array's capacity but length zero, backed by stack memory. `Sum` and `Encode`
append into that capacity, and the resulting slices point into the stack array —
so the compiler can prove there is no heap escape.

### 2. Deadline Amortization — Cut SetDeadline ~98%

{{< diagram src="/diagrams/12b-deadline-amortization.svg" alt="Phased deadline amortization" caption="Figure 3: Phased deadlines cut kernel syscalls from 200/sec to 3/sec" >}}

A **deadline** is the point at which the network layer gives up on a connection.
Resetting it before every read and write is the safe, obvious thing to do — and
it costs two kernel syscalls per chunk. We amortized the cost by setting one
absolute deadline per *phase* of the connection instead.

```go
// BEFORE: SetDeadline on every read/write (2 syscalls per chunk)
conn.SetDeadline(time.Now().Add(5 * time.Second))
n, err := conn.Read(buf)
conn.SetDeadline(time.Now().Add(5 * time.Second))
conn.Write(buf[:n])

// AFTER: phased absolute deadlines, one SetDeadline per phase
// Handshake: 10s absolute
conn.SetDeadline(time.Now().Add(10 * time.Second))
// ... handshake ...

// Metadata: 60s absolute (reuses the same deadline)
conn.SetDeadline(time.Now().Add(60 * time.Second))
// ... metadata ...

// Data transfer: dynamic, based on observed throughput
deadline = calculateTransferDeadline(bytesRemaining, throughput)
conn.SetDeadline(deadline)
// ... streaming ...
```

**Result**: from 200 `SetDeadline` calls per second per connection to 3. The
syscall overhead all but vanishes, without weakening the Slowloris defenses —
the phases still bound how long a stalled client can hold a slot.

### 3. Combined Metadata Reads

{{< diagram src="/diagrams/12c-combined-reads.svg" alt="Combined Bbolt metadata views" caption="Figure 4: Collapsing three separate Bbolt transactions into a single atomic read view" >}}

Our embedded key-value store exposes a **view**: a read-only, snapshot-consistent
transaction. Each view takes a lock. We were opening three separate views per
write to read three buckets — three locks, three snapshots — when one view can
read all three.

```go
// BEFORE: 3 separate store views per write
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

**Result**: three times fewer transactions and lock acquisitions, and a third of
the contention.

## The Measurement Contract (Non-Negotiable)

No "it's probably faster" claims. Ever.

```bash
# Every PR must pass the benchstat gate
go test -bench=. -benchmem -count=10 ./...
benchstat old.txt new.txt
```

`benchstat` is the standard Go tool that compares two sets of benchmark runs and
tells you whether a difference is statistically real or just noise. A
**regression gate** turns that comparison into a build condition: if a branch
allocates more or runs measurably slower than the base, CI fails and the merge
stops. A **profile baseline** is a saved CPU or memory profile that every later
change is judged against.

| Requirement | Tool | Gate |
|-------------|------|------|
| No allocation regression | `benchstat` | CI fails if allocation delta > 0% |
| No latency regression | `benchstat` | CI fails if ns/op delta > 5% |
| Profile baseline | `go test -cpuprofile` | file-based profiles only |
| History tracking | checked-in benchmark history | per-benchmark deltas over time |

The "file-based profiles only" rule is a security decision, not a convenience
one: a live profiling endpoint on a server with no authentication is a
remote-code-execution-class surface, so we never expose one on the data path.
The exact rule number and the paths live in the References section.

## ⚡↔🛡 Trade-off Posture

Bolt optimizes *inside* hot paths but never *at the expense* of invariants:

| Invariant | Never Compromised |
|-----------|-------------------|
| Verify-on-read | stays ON (see [007](007-at-rest-integrity-and-gc.md)) |
| Bounds checking | all inputs validated, no unsafe casts |
| Integrity / crypto / placement core | compiled-in, auditable |

**Principle: speed comes from *removing waste*, not *skipping checks*.**

## When NOT to Apply Bolt Patterns

| Pattern | Don't Use When |
|---------|----------------|
| Stack-buffer formatting | low-frequency paths (startup, config, admin APIs) |
| Deadline amortization | per-request deadlines are required (multi-tenant isolation) |
| Combined reads | the buckets live on different storage backends |
| Zero-escape hashing | non-hot paths, or when the hash must escape (e.g. returned to a caller) |

## References / Dig deeper

- The two mindsets: [docs/STANDARDS.md](../../STANDARDS.md) — ⚡ Bolt and 🛡 Sentinel.
- Measurement gate: [025: The Benchstat Gauntlet](025-benchmark-benchstat-gate.md).
- Profiling baseline: [042: Perf Profiling Baseline](042-perf-profiling-baseline.md).
- Integrity and verify-on-read: [007: At-Rest Integrity and GC](007-at-rest-integrity-and-gc.md).
- Transport: [003: TCP → QUIC](003-transport-tcp-to-quic.md).
- Applied patterns: [047](047-bolt-s3-copyresult-time-alloc.md),
  [048](048-bolt-listparts-appendformat.md),
  [049](049-eliminate-redundant-time-format-allocations.md),
  [045](045-bolt-lastmodified-header.md),
  [036](036-s3-listxml-appendformat-optimization.md).
- Benchmark harness and history: the CI benchmark-compare workflow,
  `docs/PERFORMANCE.md`, and the checked-in benchmark history data file.
- The networked-profiler prohibition is recorded as Rule 75 in the steering
  rules; the profile-baseline spec is `openspec/changes/perf-profiling-baseline`.
