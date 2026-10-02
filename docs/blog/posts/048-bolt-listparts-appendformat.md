---
title: "⚡ Bolt: Eliminating the Last Remaining S3 XML Time Allocation"
date: "2026-09-08T11:29:58Z"
draft: false
post_type: issue
tags:
  - bolt
  - performance
categories:
  - s3
  - transport
  - performance
summary: "How we eliminated the final per-response heap allocation in S3 XML rendering by swapping time.Format for time.AppendFormat in the ListParts handler. Teaches: the stack-buffer time formatting pattern, why Format allocates, and how microbenchmarks prove the win."
artifacts:
  - type: spec
    path: openspec/changes/bolt-listparts-appendformat
related: ["024-bolt-performance-engineering", "047-bolt-s3-copyresult-time-alloc", "045-bolt-lastmodified-header"]
difficulty: "intermediate"
pattern: "zero-alloc"
teaches:
  - "time.AppendFormat vs time.Format"
  - "stack buffer allocation pattern"
  - "microbenchmark-driven optimization"
---

For a while, every S3 protocol response we rendered carried a hidden cost: a
fresh heap allocation just to format a timestamp. We had been removing them one
by one — the `CopyObjectResult` body, then the HTTP `Last-Modified` header — and
this change removed the last one in the XML path.

An **allocation** is memory claimed from the heap, the pool the garbage collector
manages. Under sustained object-store traffic every allocation adds GC pressure
and latency to a **hot path**, the code that runs on nearly every request. The
S3 gateway builds its protocol responses as hand-written XML, so every timestamp
rendered into that XML is a candidate for this fix.

## Why time.Format Allocates

`time.Format` returns a `string`. In Go, producing that string allocates a new
slice on the heap — `Format` has no way to write into caller-owned memory,
because the caller has not provided any.

So in the `ListParts` handler:

```go
tstr := time.Now().UTC().Format(time.RFC3339)
```

every `ListParts` request paid one 24-byte heap allocation just to render the
timestamp inside each `<LastModified>` element. Polling multipart-upload progress
is a common repeated SDK and CLI pattern, so this allocation landed squarely on a
hot path.

## The Fix: AppendFormat Into a Stack Buffer

`time.AppendFormat` is the zero-allocation sibling: it appends the formatted time
into a caller-provided `[]byte` and returns the grown slice. Give it a
stack-allocated `[32]byte` array and there is no heap involvement at all:

```go
var timeBuf [32]byte
tstr := time.Now().UTC().AppendFormat(timeBuf[:0], time.RFC3339)
```

One subtlety: `AppendFormat` returns a `[]byte`, not a `string`, so the XML write
site switches from `buf.WriteString(tstr)` to `buf.Write(tstr)`:

```go
buf.WriteString(`<LastModified>`)
buf.Write(tstr)
buf.WriteString(`</LastModified>`)
```

The emitted bytes are identical — same `time.RFC3339` layout — so the AWS CLI and
every SDK parse the response exactly as before. There is zero protocol change.

## Proof, Not Claims

Following the house rule that performance changes ship with evidence, we added a
paired microbenchmark:

```
BenchmarkListPartsTime_AppendFormat-8  10762976  120.7 ns/op  0 B/op  0 allocs/op
BenchmarkListPartsTime_Format-8         6846415  163.3 ns/op  24 B/op  1 allocs/op
```

0 allocations per operation versus 1 (24 bytes), and roughly 26% faster
formatting. The full 289-test transport suite passes under the race detector.

## The Pattern

The general rule we codified: **scan loops that generate XML or JSON arrays for
`time.Now()` or `strconv.Itoa` calls. Hoist constant strings out of the loop, and
convert `time.Format` to `time.AppendFormat` with a stack buffer.** This change
applied that rule to the timestamp in the `ListParts` handler.

> **Pattern: Stack-Buffer Time Formatting**
> When a hot path formats a timestamp, write it with `time.AppendFormat` into a
> stack-allocated byte array instead of calling `time.Format`.
>
> **Applies when**: high-frequency rendering — protocol responses, HTTP headers,
> log lines.
> **Doesn't apply**: one-off or user-facing formatting, where the allocation is
> negligible.

## When NOT to Use This

- Low-frequency admin or startup paths.
- User-facing display dates, which are clearer as strings and not hot.
- Anywhere the surrounding API contract requires a `string`; the allocation is
  unavoidable at that boundary.

## References / Dig deeper

- Mindset standards: [docs/STANDARDS.md](../../STANDARDS.md) — ⚡ Bolt / 🛡 Sentinel.
- Bolt engineering overview: [024](024-bolt-performance-engineering.md).
- The prior XML and header changes:
  [047](047-bolt-s3-copyresult-time-alloc.md),
  [045](045-bolt-lastmodified-header.md).
- The change spec: `openspec/changes/bolt-listparts-appendformat`.
- The paired microbenchmarks: `BenchmarkListPartsTime_AppendFormat` vs
  `BenchmarkListPartsTime_Format`, in the transport test suite.
