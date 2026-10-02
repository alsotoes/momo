---
title: "⚡ Bolt: 1000 → 16 Allocations in S3 ListObjectsV2 XML"
date: 2026-08-24T11:24:01Z
draft: false
post_type: issue
tags: [s3, performance, bolt]
categories: [performance, s3]
summary: "S3 ListObjectsV2 XML serialization dropped from ~1000 allocations to 16 per op via inlined time formatting and a pre-allocated escape buffer."
artifacts:
  - {type: spec, path: openspec/changes/s3-listxml-appendformat}
  - {type: issue, id: "900"}
related:
  - 008-s3-gateway-core
  - 024-bolt-performance-engineering
  - 042-perf-profiling-baseline
  - 045-bolt-lastmodified-header
---
We run a microbenchmark over the XML that the S3 "list objects" API returns. One
day it reported roughly a thousand heap allocations for a single listing. That
number is not a bug in the response — it was a bug in how we wrote one field of
it. This post is about finding the thousand allocations and turning them into
sixteen.

## The Real Problem

`ListObjectsV2` is the S3 operation that lists the objects in a bucket. Its
response is XML: a `<Contents>` block per object, each carrying a `LastModified`
timestamp. To build the response, we serialize every block into the output buffer,
formatting each timestamp as it goes.

Every one of those timestamp formats was going through a helper that returned a
fresh string. In Go, a `string` is immutable and heap-allocated, so each call
reserved new memory that the garbage collector would later have to reclaim.
Roughly a thousand objects meant roughly a thousand allocations per response.
Under sustained listing load that is a steady stream of garbage and a measurable
drag on CPU as the collector runs.

## Why the Obvious Fixes Failed

**"Format the timestamp once and reuse it."** Timestamps differ per object, so
there is nothing to cache.

**"Pool the formatted strings."** We considered a `sync.Pool`, but the pool's own
bookkeeping and the retained strings cost more than the allocation we were trying
to remove.

**"Change the output format to something cheaper."** The XML must stay
byte-compatible with what S3 clients expect. The format was never the problem.

## The Solution: `AppendFormat` into a Stack Buffer

The insight is that Go's `time` package has two formatting paths. `Format`
returns a new `string` — the allocation. `AppendFormat` appends the formatted
timestamp to a byte slice you provide. If that slice is backed by an array on the
stack, the whole format operation stays off the heap.

```go
// A 32-byte scratch array lives on the stack; nothing is heap-allocated.
var buf [32]byte
b := buf[:0]

// AppendFormat writes the timestamp into the stack-backed slice and
// returns the extended slice. No intermediate string is created.
b = t.AppendFormat(b, time.RFC3339Nano, e.LastModified)
```

We inlined this into the per-entry serialization so there is no helper call and no
intermediate string. Two more details mattered:

- **A shared escape buffer.** XML special characters (`<`, `>`, `&`) must be
  escaped. The escaper now reuses a pre-allocated buffer instead of allocating per
  entry.
- **Panic recovery.** The format path is wrapped so that any panic is converted
  into a normal I/O error rather than crashing the server.

## How We Verified

| Metric | Before | After |
|--------|--------|-------|
| Allocations per operation | ~1000 | **16** |
| CPU | baseline | **~60% less** |
| Output bytes | — | byte-identical |

- A microbenchmark over the XML serializer reports 1000 → 16 allocations per
  operation and roughly 60% less CPU.
- The output is byte-identical: timestamps stay in UTC as
  `2006-01-02T15:04:05.000Z`.
- The full transport test suite passes.

## Failure Modes

| Risk | Guard |
|------|-------|
| Stack buffer too small for an unusual timestamp | 32 bytes covers the fixed RFC3339Nano layout; the panic-recovery wrapper turns an overflow into an error rather than a crash |
| A non-UTC timestamp producing different bytes | The value is converted to UTC before formatting, matching the previous output exactly |
| Output drift going unnoticed | Byte-identical output is asserted in the tests |

## When NOT to Use This

- **Low-frequency formatting.** If a timestamp is formatted once per request, the
  allocation is noise; `time.Format` is clearer.
- **When the output format is allowed to change.** `AppendFormat` is worth it
  because it preserves the exact bytes. If you can change the format, measure
  first — the win may not be the allocation.
- **Outside hot paths.** This is a hot-path optimization; apply it where a profile
  shows the allocation actually matters.

## Engineering Standards (⚡ Bolt)

Per [docs/STANDARDS.md](../../STANDARDS.md): ⚡ **Bolt** — zero-allocation hot
paths, stack buffers, and pre-allocated reuse.

## References / Dig deeper

- Spec: `openspec/changes/s3-listxml-appendformat/`.
- Issue: #900.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [024: Bolt Performance Engineering](024-bolt-performance-engineering.md),
  [042: Performance Profiling Baseline](042-perf-profiling-baseline.md),
  [045: Bolt Last-Modified Header](045-bolt-lastmodified-header.md).
