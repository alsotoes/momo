# Change: Bolt — zero-allocation AWS chunked header parsing

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1113 (tracking)

## Why

Every `aws-chunked` (streaming SigV4) upload body is framed as a sequence of
chunks, each preceded by a header line of the form
`hex-size[;chunk-signature=<sig>][;ext=v]`. `parseAWSChunkHeader` parses that
line on the S3 ingest hot path — once per chunk.

The current implementation uses `strings.Split(line, ";")`, which allocates a
slice header plus a backing array for the split parts on every call, and
`strings.TrimPrefix` to strip the `chunk-signature=` field, which allocates a
new string on every match. For a body split into ~64 KiB chunks (the aws-cli /
SDK default), that is two avoidable heap allocations per chunk — pure GC
pressure that scales with object size.

## What Changes

- In `parseAWSChunkHeader` (`src/transport/aws_chunked.go`):
  - Replace `strings.Split(line, ";")` with a `strings.IndexByte` scan that
    slices the original string for the size field and each subsequent field
    (zero copies).
  - Replace `strings.TrimPrefix(part, awsChunkSigField)` with a length-bound
    slice `part[len(awsChunkSigField):]`.
- Preserve parse semantics exactly: hex size (whitespace-trimmed, parsed
  case-insensitively), `maxAWSChunkSize` bound, `chunk-signature=` matching,
  64-byte signature length bound, and last-`chunk-signature=`-wins ordering.
- Extend `TestParseAWSChunkHeader` with edge cases (no `;`, trailing `;`,
  `;;`, extension fields around the signature, surrounding whitespace,
  multiple signatures, empty signature, empty size, non-hex, negative).
- Add `BenchmarkParseAWSChunkHeader` to make the allocation claim reproducible.
- Append a `.jules/bolt.md` learning entry (Rule 44 append-only).

## Non-Goals

- No change to the de-framing state machine, signature verification, chunk
  size limits, or the `readHeaderLine` bounds.
- No wire/protocol changes, no config changes.
- Other `strings.Split` sites (`sigv4.go`, etc.) are out of scope.

## Impact

- **Affected Specs:** `specs/bolt-aws-chunked-zero-alloc/spec.md`.
- **Performance:** Per-chunk header parse goes from 1 alloc/op (32 B) to
  0 allocs/op (measured ~280–520 ns/op → ~58–69 ns/op on the target host).
- **Correctness:** Parse results are unchanged; all existing aws-chunked tests
  must pass, plus the new edge-case table.
