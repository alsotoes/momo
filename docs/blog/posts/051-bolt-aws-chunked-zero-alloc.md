---
title: "⚡ Bolt: Zero-Allocation Parsing of AWS Chunked Headers"
date: "2026-10-01T11:32:41Z"
draft: false
post_type: issue
tags:
  - bolt
  - performance
  - s3
categories:
  - s3
  - transport
  - performance
summary: "How we removed the last heap allocation from the aws-chunked ingest path by replacing strings.Split and strings.TrimPrefix with a strings.IndexByte scan. Teaches: what a streaming S3 upload body looks like, why Split allocates per chunk, and how a microbenchmark proves the win."
artifacts:
  - type: spec
    path: openspec/changes/bolt-aws-chunked-zero-alloc
related: ["024-bolt-performance-engineering", "039-signed-payload-sse-s3", "040-aws-chunked-streaming", "045-bolt-lastmodified-header", "047-bolt-s3-copyresult-time-alloc", "048-bolt-listparts-appendformat", "049-eliminate-redundant-time-format-allocations", "052-bolt-etag-matches-zero-alloc"]
difficulty: "intermediate"
pattern: "zero-alloc"
teaches:
  - "aws-chunked framing"
  - "strings.IndexByte vs strings.Split"
  - "microbenchmark-driven optimization"
---

An **allocation** is memory claimed from the heap, the pool the garbage
collector manages. Every allocation on a **hot path** — code that runs on
nearly every request — adds GC pressure and latency. We had been removing these
one at a time from the S3 gateway; this change removed two more, from a place
that runs *many times per object*: the parser for streaming upload headers.

## The Problem: One Allocation Per Chunk

An S3 client can upload a body as a **streaming, signed payload**. Instead of
sending the whole object and a signature up front, it sends the object in
**chunks** — typically 64 KiB each — so the server can verify data as it
arrives. Each chunk is preceded by a small text header line:

```
10000;chunk-signature=0123abcd...
```

The first field is the chunk size in hexadecimal; the rest are semicolon-
separated fields, of which `chunk-signature=` carries the per-chunk signature.

Parsing that line is not a one-time cost. A 100 MB object split into 64 KiB
chunks arrives as roughly 1,600 chunk headers, each parsed once on the ingest
path. The old implementation parsed each line with:

```go
parts := strings.Split(line, ";")            // allocates a slice + backing array
...
sig = strings.TrimPrefix(part, "chunk-signature=")  // allocates a new string
```

`strings.Split` allocates a slice header and a backing array to hold the parts,
even when the caller only needs the first two fields. `strings.TrimPrefix`
allocates again whenever it matches. Two heap allocations per chunk, multiplied
by the chunk count, is pure waste — the data is already in memory as a string,
and all we need are slices of it.

## The Fix: Scan and Slice, Don't Copy

Go strings are immutable and cheap to **slice**: `s[i:j]` produces a new string
header that shares the same underlying bytes, with no copy and no heap
allocation. `strings.IndexByte` finds a byte without allocating. Together they
replace `Split` and `TrimPrefix` with a single pass and zero allocations:

```go
var sizeStr, rest string
if idx := strings.IndexByte(line, ';'); idx != -1 {
    sizeStr = line[:idx]   // slice, no copy
    rest = line[idx+1:]
} else {
    sizeStr = line
}

size, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 16, 64)
// ...
sig := ""
for len(rest) > 0 {
    var part string
    if idx := strings.IndexByte(rest, ';'); idx != -1 {
        part = rest[:idx]
        rest = rest[idx+1:]
    } else {
        part = rest
        rest = ""
    }
    if strings.HasPrefix(part, awsChunkSigField) {
        sig = part[len(awsChunkSigField):]   // slice off the prefix, no copy
        // ... length bound unchanged
    }
}
```

The loop walks the same fields in the same order as the old range over
`parts[1:]`, so the semantics are unchanged — including the subtle rule that if
a line somehow carries two `chunk-signature=` fields, the **last one wins**.

> **Principle — prefer slicing to splitting.**
> When you only need offsets into a string, use `IndexByte` + slicing. Reserve
> `Split` (and `TrimPrefix`) for the cases where you genuinely need independent
> substrings, and remember they never land on a hot path by accident.

## Verifying It

A refactor of a parser must prove two things: nothing broke, and the cost went
down. The first is a table test. We extended `TestParseAWSChunkHeader` with the
edge cases the old code and the new code must agree on: a line with no `;`, a
trailing `;`, an empty field (`;;`), extension fields before and after the
signature, whitespace around the size, multiple signatures, an empty signature,
an empty size, a non-hex size, and a negative size. Both implementations return
the same result for every one.

The second is a **microbenchmark** — a small benchmark that isolates one
function. `BenchmarkParseAWSChunkHeader` reports allocations per operation:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| `strings.Split` + `TrimPrefix` (before) | ~280–520 | 32 | 1 |
| `IndexByte` + slice (after) | ~58–69 | 0 | 0 |

The allocation count is the number that matters. Driving it to zero means the GC
never sees these calls at all, no matter how many chunks an upload contains.

## Failure Modes

| Risk | Guard |
|---|---|
| A subtle parse difference changes behavior | Edge-case table test asserts old and new agree on every input |
| Signature length bound is lost in the rewrite | The 64-byte limit is preserved and covered |
| A path inside a rebuilt string leaks out | Fields are slices of the original line, bounded by the existing header-line limit |
| The win is only a microbenchmark artifact | The de-framing benchmarks (`BenchmarkAWSChunkedReaderSigned/Unsigned`) exercise the full path |

## When NOT to Use This

When you need real substrings. `Split` returns owned, independent strings; a
slice aliases the parent and keeps it alive. If the parsed pieces must outlive
the source line — or be modified — copy them. Here the fields are consumed
immediately and the source line is bounded to 1 KiB, so slicing is both safe and
correct. As always, measure first: reach for this pattern only after a profile
points at the parser.

## References / Dig deeper

- The standard: [docs/CORE/STANDARDS.md](../../STANDARDS.md)
- Related Bolt posts: [024](024-bolt-performance-engineering.md) performance
  mindset, [045](045-bolt-lastmodified-header.md) header formatting,
  [047](047-bolt-s3-copyresult-time-alloc.md) XML timestamps,
  [048](048-bolt-listparts-appendformat.md) the last XML allocation,
  [049](049-eliminate-redundant-time-format-allocations.md) SigV4 datestamps.
- The aws-chunked framing spec: [040](040-aws-chunked-streaming.md) and
  [039](039-signed-payload-sse-s3.md) in the S3 series.
- Tracking issue: [#1113](https://github.com/alsotoes/momo/issues/1113).
