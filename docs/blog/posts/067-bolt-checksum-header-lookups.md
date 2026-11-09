---
title: "Bolt: Four Allocations to Learn Nothing"
date: "2026-11-08T11:24:10Z"
draft: false
post_type: issue
tags:
  - bolt
  - s3
  - performance
categories:
  - performance
summary: "Every S3 request asked four times whether it carried a checksum header, and built each header name on the spot to ask. The answer was almost always no, and the question cost four heap allocations. A static table makes the same question free."
artifacts: [{type: spec, path: openspec/changes/bolt-canonical-checksum-headers}]
related: ["051-bolt-aws-chunked-zero-alloc", "052-bolt-etag-matches-zero-alloc", "045-bolt-lastmodified-header"]
difficulty: "beginner"
pattern: "allocation-elimination"
teaches:
  - "Why constructing a map key per lookup allocates"
  - "How a static table removes the allocation"
  - "Why hot-path no-answer questions still cost"
---

The S3 gateway supports four integrity-checksum algorithms. To find out whether a
request carried one, it asked four questions: is there a CRC32C header, a CRC32
header, a SHA-1 header, a SHA-256 header? Almost every request answered no to all
four — most clients never send a checksum at all.

The cost of asking was not the lookup. A Go HTTP header map is keyed by
canonicalized names (`X-Amz-Checksum-Sha256`), so the code built each name with
string concatenation and passed it through canonicalization: one allocation per
question, four per request, millions of requests. The gateway was paying four
allocations to learn, reliably, that there was nothing to do.

## The Fix Is a Table

The set of names is fixed and known at compile time. There is no reason to
construct them:

```go
var checksumRequestHeaders = []struct {
    algo   string
    header string
}{
    {checksumAlgoCRC32c, "X-Amz-Checksum-Crc32c"},
    {checksumAlgoCRC32,  "X-Amz-Checksum-Crc32"},
    {checksumAlgoSHA1,   "X-Amz-Checksum-Sha1"},
    {checksumAlgoSHA256, "X-Amz-Checksum-Sha256"},
}
```

The loop stays — the precedence order is a real semantic (CRC32C wins over CRC32)
and should look like a loop. Only the per-request string building disappears,
because the literals are already in canonical form and Go's header lookup returns
them unchanged when they are.

## Measured, Not Assumed

A benchmark that probes the same request a thousand times makes the difference
visible without a profiler:

| Variant | Time | Memory | Allocations |
|---|---|---|---|
| Dynamic construction (baseline) | ~930 ns/op | 96 B/op | 4 allocs/op |
| Pre-canonicalized table | ~450 ns/op | 0 B/op | 0 allocs/op |

Half the time and none of the garbage, on a path that runs for every object
ingress. The test suite also pins that the literals really are canonical (by
comparing them to `http.CanonicalHeaderKey` of the response header name), because
a typo in a literal is a silent lookup miss — the exact class of bug that makes
an optimization worse than the code it replaced.

## References / Dig deeper

- OpenSpec change: `openspec/changes/bolt-canonical-checksum-headers/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1275
- Engineering standard: `docs/CORE/STANDARDS.md` (⚡ Bolt zero-allocation rules)
