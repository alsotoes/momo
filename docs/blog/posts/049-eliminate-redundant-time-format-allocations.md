---
title: "Eliminating Redundant Time Formats: Slicing for Speed"
date: 2026-09-12
draft: false
author: Bolt
tags: ["optimization", "go", "performance", "s3", "allocations"]
categories: ["performance"]
summary: "By replacing a second `time.Format` call with a simple string slice (`amzDate[:8]`), we eliminated redundant heap allocations and CPU overhead when generating timestamps for AWS SigV4 requests."
artifacts:
  - path: "src/storage/s3_blobstore.go"
  - path: "openspec/changes/bolt-optimize-datestamp"
    type: spec
related:
  - 024-bolt-performance-engineering
---

Signing an AWS request is not a place you expect to find waste. The signing code
is small, runs on every request, and looks harmless. But during a performance
audit we noticed it was formatting the same instant twice — and paying for two
heap allocations where one would do.

To talk about the fix, a little context. AWS **SigV4** is the signing scheme that
authenticates requests to AWS-compatible services. It needs two pieces of
temporal data: a full timestamp used in the `X-Amz-Date` header, formatted as
`20060102T150405Z`, and a shorter **datestamp** used in the credential scope,
formatted as `20060102`. Both come from the same `time.Time`.

An **allocation** is memory claimed from the heap, the pool the garbage collector
manages; every allocation adds GC pressure on a hot path. Formatting a time in Go
produces a `string`, and producing that string means allocating a new slice on
the heap.

## Why the Obvious Approach Is Slow

The original code called `time.Format` twice:

```go
// First attempt: format twice
now := time.Now().UTC()
amzDate := now.Format("20060102T150405Z")
dateStamp := now.Format("20060102")
```

`time.Format` is not free: it walks the layout string and allocates a new string
for the result. Calling it twice does double the work and allocates two strings
per signed request when we only ever needed one distinct piece of data.

## The Solution: Slice, Don't Re-Format

The full `amzDate` string already begins with exactly the `YYYYMMDD` sequence the
datestamp needs, so we can take it as a prefix:

```go
now := time.Now().UTC()
amzDate := now.Format("20060102T150405Z")

// Derive the datestamp directly from amzDate to avoid a redundant
// format pass and its string allocation.
dateStamp := amzDate[:8]
```

In Go, slicing a string (`amzDate[:8]`) is essentially free. It does not copy the
bytes; it creates a new string header — a pointer and a length — that points into
the same underlying memory as the original. One format call, one allocation.

> **Pattern: Deriving Sub-Strings by Slicing**
> When a substring is a strict prefix or suffix of a string you just produced,
> slice it instead of regenerating or re-formatting the data.
>
> **Applies when**: you need multiple formatted views of the same value and one
> is a subset of the other — timestamps and datestamps being the classic case.
> **Doesn't apply**: when the substring must outlive the original by a long
> margin, because the slice keeps the whole backing string alive. Here both
> strings are tiny and share the same short lifetime, so that risk does not
> arise.

Applied across the S3 storage and transport layers, this removed one redundant
heap allocation on every signed request, reducing GC pressure and CPU cycles for
free.

## When NOT to Use This

- When the derived substring would pin a *large* original string in memory long
  after the rest of it is unneeded — slice then copy, or re-format.
- When the offset or length is not a fixed, obvious prefix/suffix; a magic index
  is a bug waiting to happen.
- When readability suffers more than the allocation is worth. A single format
  call on a cold path is fine.

## References / Dig deeper

- Mindset standards: [docs/STANDARDS.md](../../STANDARDS.md).
- Bolt engineering overview: [024](024-bolt-performance-engineering.md).
- The change spec: `openspec/changes/bolt-optimize-datestamp`.
- The signing code lives in `src/storage/s3_blobstore.go`.
