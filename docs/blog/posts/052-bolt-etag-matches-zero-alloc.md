---
title: "⚡ Bolt: Zero-Allocation Parsing of ETag Lists"
date: "2026-10-06T11:16:37Z"
draft: false
post_type: issue
tags:
  - bolt
  - performance
  - s3
  - http
categories:
  - s3
  - transport
  - performance
summary: "How we removed the heap allocation from ETag list parsing on the S3 conditional-request path by replacing strings.Split with a strings.IndexByte scan. Teaches: what If-Match/If-None-Match actually carry, why Split allocates per request, and how a microbenchmark proves the win."
artifacts:
  - type: spec
    path: openspec/changes/bolt-etag-matches-zero-alloc
related: ["024-bolt-performance-engineering", "048-bolt-listparts-appendformat", "051-bolt-aws-chunked-zero-alloc"]
difficulty: "intermediate"
pattern: "zero-alloc"
teaches:
  - "HTTP conditional requests and ETag lists"
  - "strings.IndexByte vs strings.Split"
  - "microbenchmark-driven optimization"
---

HTTP lets a client ask a server a precise question: *"only send me this object
if it has changed."* The client answers with the last **entity tag** (ETag) it
saw, and the server compares it against the current one. Done well, this turns a
full object transfer into a few bytes. Done carelessly, the comparison itself
allocates memory on every request — which is what this change fixed.

## The Problem: An Allocation on Every Conditional Request

An ETag is an opaque identifier for a specific version of an object, usually a
hash. A client sends it back in one of two headers:

- `If-None-Match` — *"send the body only if its tag is **not** in this list"*
  (the basis of `304 Not Modified` and caching).
- `If-Match` — *"apply this write only if the tag **is** in this list"*
  (the basis of safe conditional writes).

The value is not always one tag. HTTP allows a **comma-separated list**:

```
If-None-Match: "aaa111", "bbb222", W/"ccc333"
```

A tag may be quoted, and a `W/` prefix marks a **weak** validator — one that
matches when the content is semantically equivalent, not byte-identical. There
is also the wildcard `*`, meaning *"any version."* So the server has to walk the
list, normalise each entry, and compare it against the object's current hash.

The old implementation walked the list with `strings.Split`:

```go
for _, item := range strings.Split(header, ",") {
    item = strings.TrimSpace(item)
    if item == "*" { return true }
    item = strings.TrimPrefix(item, "W/")
    item = strings.TrimSpace(item)
    item = strings.Trim(item, `"`)
    if item == etag { return true }
}
```

`strings.Split` allocates a slice header **and a backing array** to hold the
parts — even though the caller only ever looks at one entry at a time and then
discards it. On a busy gateway serving cached reads, that is one heap
allocation per conditional request, feeding the garbage collector for no
reason: the bytes are already in memory as a single string.

## The Fix: Scan and Slice, Don't Copy

Go strings are immutable and cheap to **slice**: `s[i:j]` produces a new string
header that shares the same bytes — no copy, no heap allocation.
`strings.IndexByte` finds a byte without allocating. Together they replace
`Split` with a single pass:

```go
for len(header) > 0 {
    var item string
    if idx := strings.IndexByte(header, ','); idx == -1 {
        item = header          // last field
        header = ""
    } else {
        item = header[:idx]    // slice, no copy
        header = header[idx+1:]
    }

    item = strings.TrimSpace(item)
    if item == "*" { return true }
    item = strings.TrimPrefix(item, "W/")
    item = strings.TrimSpace(item)
    item = strings.Trim(item, `"`)
    if item == etag { return true }
}
return false
```

The normalisation steps are untouched — the same `TrimSpace`, `*`, `W/`, and
quote-stripping logic runs on each entry, in the same order. Only the way the
entries are produced changed: from an allocated slice to a cursor over the
original string.

> **Principle — prefer slicing to splitting.**
> When you only need offsets into a string, reach for `IndexByte` + slicing.
> Reserve `Split` for cases where you genuinely need independent substrings,
> and treat every `Split` on a request path as a small tax the GC collects.

## Verifying It

Two things must hold after a parser rewrite: nothing broke, and the cost fell.

The first is a **table test**. We converted the ETag-match test into a table
covering the cases the old and new code must agree on: quoted and unquoted
tags, weak tags (`W/"…"`, and `W/ "…"` with a space), the wildcard alone and
inside a list, leading and trailing spaces, a trailing comma, a tag absent from
the list, an empty header, and an empty object tag. Every case returns the same
result as before.

The second is a **microbenchmark** — a benchmark that isolates one function.
`BenchmarkEtagMatches` reports allocations per operation, with
`BenchmarkEtagMatchesSplit` kept as the "before" baseline:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| `strings.Split` (before) | ~257–296 | 80 | 1 |
| `IndexByte` + slice (after) | ~127–181 | 0 | 0 |

The allocation count is the number that matters. Driving it to zero means the
GC never sees these calls — regardless of how many conditional requests arrive.

## Failure Modes

| Risk | Guard |
|---|---|
| A subtle parse difference changes matching | Table test asserts old and new agree on every input |
| A weak or wildcard tag stops matching | Dedicated cases for `W/` and `*` (alone and in a list) |
| A sliced entry aliases and outlives the header | Entries are consumed immediately; the header string is request-scoped |
| The win is only a microbenchmark artifact | The split baseline runs the same header, so the delta is the allocation, not the input |

## When NOT to Use This

When the parsed pieces must **outlive** the source or be **modified**. A slice
aliases the parent string and keeps it alive; `Split` returns owned copies. If
an entry is stored beyond the request, or written to, copy it. Here each entry
is normalised and compared in place, so slicing is safe. And as always: profile
first. This pattern earns its place only after a measurement points at the
parser.

## References / Dig deeper

- The standard: [docs/STANDARDS.md](../../STANDARDS.md)
- Related Bolt posts: [024](024-bolt-performance-engineering.md) performance
  mindset, [048](048-bolt-listparts-appendformat.md) the last XML allocation,
  [051](051-bolt-aws-chunked-zero-alloc.md) streaming-header parsing.
- Tracking issue: [#1133](https://github.com/alsotoes/momo/issues/1133).
