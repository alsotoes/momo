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

## The Last Allocation in the XML Hot Path

Our S3 gateway renders protocol responses as hand-built XML. Over the last few
changes we've been systematically removing heap allocations from those
rendering paths, because under sustained object-store traffic every allocation
adds GC pressure and latency to a hot request path. We removed the allocation
from `CopyObjectResult`, then from HTTP `Last-Modified` headers — and in this
change we removed the last one: the per-response timestamp in the
`ListParts` result.

## Why time.Format Allocates

`time.Format` returns a `string`. In Go, producing that string allocates a new
slice on the heap — there is no way for `Format` to write into caller-owned
memory, because the caller hasn't provided any.

So in `handleListParts`:

```go
tstr := time.Now().UTC().Format(time.RFC3339)
```

every `ListParts` request pays one 24-byte heap allocation just to render the
timestamp into each `<LastModified>` element. Polling multipart-upload progress
is a common repeated SDK/CLI pattern, so this allocation lands on a hot path.

## The Fix: AppendFormat Into a Stack Buffer

`time.AppendFormat` is the zero-alloc sibling: it appends the formatted time
into a caller-provided `[]byte` and returns the grown slice. Give it a
stack-allocated `[32]byte` array and there is no heap involvement at all:

```go
var timeBuf [32]byte
tstr := time.Now().UTC().AppendFormat(timeBuf[:0], time.RFC3339)
```

One subtlety: `AppendFormat` returns a `[]byte`, not a `string`. The XML write
site must switch from `buf.WriteString(tstr)` to `buf.Write(tstr)`:

```go
buf.WriteString(`<LastModified>`)
buf.Write(tstr)
buf.WriteString(`</LastModified>`)
```

The emitted bytes are identical — same `time.RFC3339` layout — so aws-cli and
every SDK parse the response exactly as before. Zero protocol change.

## Proof, Not Claims

Following the house rule that perf changes ship with evidence, we added a
paired microbenchmark (`BenchmarkListPartsTime_AppendFormat` vs
`BenchmarkListPartsTime_Format`):

```
BenchmarkListPartsTime_AppendFormat-8  10762976  120.7 ns/op  0 B/op  0 allocs/op
BenchmarkListPartsTime_Format-8         6846415  163.3 ns/op  24 B/op  1 allocs/op
```

0 allocs/op versus 1 alloc/op (24 B), and roughly 26% faster formatting. The
full 289-test transport suite passes with `-race`.

## The Pattern

The general rule we've codified in our learning files: **scan loops generating
XML/JSON arrays for `time.Now()` or `strconv.Itoa` calls. Hoist constant
strings out of the loop, and convert `time.Format` to `time.AppendFormat` with
a stack buffer.** This change applied that to the timestamp in `handleListParts`.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt and 🛡 Sentinel
engineering principles this change follows.