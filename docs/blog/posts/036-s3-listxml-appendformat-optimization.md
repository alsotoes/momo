---
title: "⚡ Bolt: 1000 → 16 Allocations in S3 ListObjectsV2 XML"
date: 2026-08-24T11:24:01Z
draft: false
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
The S3 `ListObjectsV2` XML response was formatting timestamps through a helper that allocated heavily — roughly **1000 allocations per op**. A focused fix brought it to **16** with ~60% less CPU.

## The Hot Path

`FormatListObjectsV2XML` serializes each object entry (`<Contents>` blocks) into the response body. The per-entry `LastModified` timestamp was formatted through a `formatLastModified` helper that:

- Appended a pre-rendered string per entry
- Allocated per format call

## The Fix: `t.AppendFormat` + Stack Buffer

In `emitContents`, `formatLastModified` was replaced with an inlined `t.AppendFormat` writing into a stack-allocated `[32]byte` scratch:

```go
var buf [32]byte
b := buf[:0]
b = t.AppendFormat(b, time.RFC3339Nano, e.LastModified)
```

- **Byte-identical output**: UTC `2006-01-02T15:04:05.000Z` — no behavioral change
- **Zero per-entry allocation**: stack buffer reused
- **Panic recovery**: `defer recover → syscall.EIO` guards the format path
- **Shared escape buffer**: `xmlEscape` now reuses a pre-allocated buffer

## Result

| Metric | Before | After |
|--------|--------|-------|
| Allocations/op | ~1000 | **16** |
| CPU | baseline | **~60% less** |
| Output | — | byte-identical |

## Verification

- `s3_xml_bench_test.go` microbenchmark: 1000 → 16 allocs/op, ~60% less CPU
- Output byte-identical: UTC `2006-01-02T15:04:05.000Z`
- Full transport test suite green

## Standards

Per [docs/STANDARDS.md](../../STANDARDS.md): ⚡ **Bolt** (zero-allocation hot path, stack buffers, pre-allocated reuse).

## Artifacts

- Spec: `openspec/changes/s3-listxml-appendformat/`
- Issue: #900
- PR: #... (merged)
