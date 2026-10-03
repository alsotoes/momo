---
title: '⚡ Bolt: Zero-Allocation Last-Modified Headers'
date: 2026-09-01 11:19:08+00:00
draft: false
post_type: issue
tags:
- go
- bolt
- performance
- zero-alloc
- s3
categories:
- performance
summary: 'time.Format allocates a fresh string on every S3 GET/HEAD response. Swapping
  to time.AppendFormat drops that heap escape to zero — measured 32 B/op saved per
  request on the hottest read path.'
artifacts:
- type: spec
  path: openspec/changes/bolt-http-lastmodified-appendformat
- type: pr
  id: '976'
- type: issue
  id: '977'
- type: doc
  path: docs/STANDARDS.md
related:
- 024-bolt-performance-engineering
- 036-s3-listxml-appendformat-optimization
- 012-s3-integrity-checksums
- 047-bolt-s3-copyresult-time-alloc
- 048-bolt-listparts-appendformat
- 051-bolt-aws-chunked-zero-alloc
---

One allocation per request is invisible until it is not. Every S3 `GET`, `HEAD`,
range, and `304 Not Modified` response carries a `Last-Modified` HTTP header: the
timestamp of when the object was last written, which clients and caches use to
decide whether their copy is stale. Rendering that header used to call Go's
`time.Format`, which returns a brand-new string on the heap every time.

An **allocation** is memory taken from the heap, the pool the garbage collector
manages; a **heap escape** is when a value that could have lived on the stack is
forced onto the heap because it outlives the call. On the **hot path** — the code
that runs on essentially every request — that is one 32-byte allocation per
response, all of it handed to the garbage collector. On an object store, reads
are the hot path.

The fix is the same standard-library idiom we had already shipped for XML
timestamp rendering: stop asking `time.Format` for a string, and write the
formatted bytes **directly into the response buffer** instead.

## Before / After

```go
// before: heap string, then copied into the response
b = append(b, formatHTTPLastModified(meta.ModTime)...)

// after: zero-allocation append directly into the growing slice
b = time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)
```

`AppendFormat` takes the destination slice as its first argument and returns the
extended slice, so the formatted bytes land straight in the response buffer with
no intermediate string. The header output is **byte-for-byte identical** —
`http.TimeFormat`, the IMF-fixdate format the HTTP specification mandates — so
AWS SDKs and the AWS CLI parse the response exactly as before. There is no
protocol change to reason about.

## Measured, Not Guessed

The micro-benchmark isolates just the header rendering:

```
BenchmarkLastModifiedHeader_AppendFormat   462.6 ns/op   0 B/op   0 allocs/op
BenchmarkLastModifiedHeader_Format         451.3 ns/op  32 B/op   1 allocs/op
```

**32 bytes and one allocation eliminated per response** on the GET/HEAD/304
path — zero GC pressure from timestamp formatting, at no measurable throughput
cost. The tiny ns/op difference is well inside run-to-run noise; the allocation
count is the signal that matters.

## ⚡↔🛡 Trade-off Posture

Bolt optimizes *inside* hot paths, never at the expense of invariants. Here the
invariant is byte-identical header output, verified by the existing S3 test
suite — a pure allocation removal with no behavioral surface. The now-dead
`formatHTTPLastModified` helper was deleted along with it, keeping the codebase
to a single idiom for rendering HTTP times.

## When NOT to Use This

- One-off formatting in startup, shutdown, or admin paths, where the allocation
  is irrelevant.
- When you genuinely need a `string` return value for an API boundary — the
  allocation is unavoidable there, so converting is pointless churn.
- Human-readable display dates, which are not on a hot path and are clearer as a
  string.

## References / Dig deeper

- Mindset standards: [docs/STANDARDS.md](../../STANDARDS.md).
- Bolt engineering overview: [024](024-bolt-performance-engineering.md).
- The same pattern applied to XML responses:
  [047](047-bolt-s3-copyresult-time-alloc.md) and
  [048](048-bolt-listparts-appendformat.md).
- Earlier XML work:
  [036](036-s3-listxml-appendformat-optimization.md).
- Integrity and checksums: [012](012-s3-integrity-checksums.md).
- The change spec: `openspec/changes/bolt-http-lastmodified-appendformat`;
  PR [#976](https://github.com/alsotoes/momo/pull/976);
  issue [#977](https://github.com/alsotoes/momo/issues/977).
