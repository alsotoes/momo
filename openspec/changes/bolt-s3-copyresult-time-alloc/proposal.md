# Change: Bolt — eliminate time formatting allocations in S3 CopyObject XML

**Related Issues:**
- https://github.com/alsotoes/momo/issues/968 (tracking)

## Why

`FormatCopyObjectResultXML` calls `formatLastModified`, which internally invokes
`time.Format()`. `.Format()` returns a dynamically allocated string, so every S3
Copy operation response pays one heap allocation on a hot path — adding GC
pressure during sustained Copy workloads. The standard-library idiom
`AppendFormat` + a stack-allocated `[32]byte` scratch buffer writes directly
into the response `bytes.Buffer` with zero heap allocations.

## What Changes

- `src/transport/s3_communicator.go`: rebuild the `LastModified` element in
  `FormatCopyObjectResultXML` with `time.AppendFormat(timeBuf[:0], ...)` into
  the existing XML buffer, replacing the `formatLastModified(modTime)` call.
- No output-format change: the emitted timestamp keeps the exact same
  `2006-01-02T15:04:05.000Z` layout S3 clients expect.
- `.jules/bolt.md`: append a learning entry documenting the pattern (append-only,
  Rule 44).

## Non-Goals
- No behavior or wire-format change for any other S3 operation.
- No change to `formatLastModified` call sites outside CopyObject.
- No runtime/metrics surface changes; validation is via build + tests + the
  micro-benchmark in the PR description.