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
- bolt-s3-copyresult-time-alloc
---

## One allocation per request, invisible until it isn't

Every S3 `GET`, `HEAD`, range, and 304 response carries a `Last-Modified` HTTP
header. Rendering it used to go through `time.Format()`, which returns a freshly
allocated string on the heap. Under sustained object-read workloads — the
**hot path** of an object store — that is one 32-byte allocation per request,
all headed straight for the garbage collector.

The fix is the same standard-library idiom we already shipped for XML
LastModified rendering in
[`bolt-s3-copyresult-time-alloc`](bolt-s3-copyresult-time-alloc.md): stop asking
`time.Format` for a string and write **directly into the response buffer**
instead.

## Before / after

```go
// before: heap string, then copied into the response
b = append(b, formatHTTPLastModified(meta.ModTime)...)

// after: zero-allocation append directly into the growing slice
b = time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)
```

`AppendFormat` takes the destination slice as its first argument and returns the
extended slice, so the formatted bytes land straight in the response buffer with
no intermediate string. Header output is **byte-for-byte identical** —
`http.TimeFormat` (IMF-fixdate) in both cases — so AWS SDKs and aws-cli parsing
are untouched.

## Measured, not guessed

The micro-benchmark isolates just the header rendering:

```
BenchmarkLastModifiedHeader_AppendFormat   462.6 ns/op   0 B/op   0 allocs/op
BenchmarkLastModifiedHeader_Format         451.3 ns/op  32 B/op   1 allocs/op
```

**32 B / 1 alloc eliminated per response** on the GET/HEAD/304 path — zero GC
pressure from timestamp formatting, at no throughput cost.

## ⚡↔🛡 tradeoff posture

Bolt optimizes *inside* hot paths, never at the expense of invariants. Here the
guarantee is byte-identical header output verified by the existing S3 test
suite — a pure allocation removal with zero behavioral surface. The dead
`formatHTTPLastModified` helper was deleted along with it, keeping the codebase
to a single idiom for header time rendering.

## Related

Transport patterns: [003](003-transport-tcp-to-quic.md). Measurement:
[025](025-benchmark-benchstat-gate.md). Allocation hunting:
[042](bolt-s3-copyresult-time-alloc.md). Bolt mindset: [024](024-bolt-performance-engineering.md).