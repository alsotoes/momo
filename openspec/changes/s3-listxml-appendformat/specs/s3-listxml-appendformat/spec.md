> GitHub Issue URL: https://github.com/alsotoes/momo/issues/900

# s3-listxml-appendformat Specification

## Purpose
Eliminate per-element heap allocation in `FormatListObjectsV2XML` by rendering
`<LastModified>` with `time.AppendFormat` into a stack-allocated buffer instead
of `formatLastModified`'s `time.Format` (which allocates a heap string each
call).

## Behavior

### SA1: emitContents renders timestamps allocation-free
Inside the `emitContents` closure of `FormatListObjectsV2XML`, for each file:

- `t := time.Unix(0, file.ModTime).UTC()`
- `buf.Write(t.AppendFormat(timeBuf[:0], "2006-01-02T15:04:05.000Z"))`

where `timeBuf` is a stack-allocated `var timeBuf [32]byte` declared once in
`FormatListObjectsV2XML`. Output is placed directly into the shared
pre-allocated `buf`.

### SA2: Byte-for-byte output equivalence
`time.Unix(0, modTime).UTC()` formatted as `2006-01-02T15:04:05.000Z` MUST
produce the same bytes as the previous `formatLastModified` return value. No
other XML element changes.

### SA3: Safety invariants preserved
- `FormatListObjectsV2XML` keeps its `defer func(){ recover() → log + syscall.EIO }`
  zero-crash block.
- `xmlEscape` continues to receive the shared pre-allocated `*bytes.Buffer` by
  pointer (no per-element heap escape).
- Sentinel field-length checks (Name/Hash ≤ 64) unchanged.

## Performance

Add a microbenchmark pair in `src/transport`:
- `BenchmarkFormatListObjectsV2XML_AppendFormat` — full `FormatListObjectsV2XML`
  over 1000 synthetic files; expected ~16 allocs/op.
- `BenchmarkFormatListObjectsV2XML_OldFormat` — reference `time.Format` path;
  ~1000 allocs/op.

Baseline measured (i7-7700HQ, linux/amd64):
| Benchmark | ns/op | allocs/op |
|-----------|-------|-----------|
| AppendFormat | ~628068 | 16 |
| OldFormat | ~390284 (hot path only) | 1000 |
