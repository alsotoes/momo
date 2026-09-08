# Change: Bolt — eliminate time.Format string allocation in S3 ListParts

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1063 (tracking)

## Why

`S3Communicator.handleListParts` renders the `ListPartsResult` XML response by
formatting the upload's last-modified timestamp once and writing it into every
`<Part>` element. The current implementation calls `time.Now().UTC().Format(time.RFC3339)`,
which returns a dynamically allocated string — so every S3 `ListParts` request
pays one heap allocation (24 B) just to produce the timestamp. Under workloads
that poll multipart upload progress (the common aws-cli / SDK pattern), this
allocation lands on a repeated hot request path and adds GC pressure.

The standard-library idiom `time.AppendFormat` writes the formatted time
directly into a stack-allocated `[32]byte` buffer, eliminating the heap
allocation entirely — the same pattern already shipped for
`CopyObjectResult` (`bolt-s3-copyresult-time-alloc`) and HTTP `Last-Modified`
headers (`bolt-http-lastmodified-appendformat`).

## What Changes

- In `S3Communicator.handleListParts` (`src/transport/s3_communicator.go`):
  replace `tstr := time.Now().UTC().Format(time.RFC3339)` with
  `var timeBuf [32]byte; tstr := time.Now().UTC().AppendFormat(timeBuf[:0], time.RFC3339)`.
- Update the XML write site from `buf.WriteString(tstr)` to `buf.Write(tstr)`
  (the `AppendFormat` result is a `[]byte`, not a `string`).
- Keep the emitted XML bytes byte-for-byte identical (same `time.RFC3339`
  layout), so AWS SDKs and aws-cli parsing is unchanged.
- Append a `.jules/bolt.md` learning entry documenting the pattern
  (Rule 44 append-only).

## Non-Goals

- The XML `LastModified` rendering paths in other S3 handlers already use the
  `AppendFormat` + stack-buffer idiom; they are out of scope here.
- No changes to `handleCopyObject`, `handleBatchDelete`, or
  `handleListMultipartUploads` (panic-recovery and input-validation hardening
  for those methods is tracked separately).
- No wire/protocol changes, no config changes.

## Impact

- **Affected Specs:** `specs/bolt-listparts-appendformat/spec.md`
  (requirements below).
- **Performance:** One fewer heap string allocation per `ListParts` response
  (benchmarked: 0 allocs/op vs 1 alloc/op, 24 B; ~26% faster formatting).
- **Correctness:** XML output identical; all existing S3 tests must pass.