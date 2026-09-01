# Tasks — Bolt: S3 CopyObject time-format allocation removal

## Phase 1 — Implementation
- [x] Rewrite `LastModified` rendering in `FormatCopyObjectResultXML` using
      `time.AppendFormat` + `var timeBuf [32]byte` into the XML buffer.
- [x] Remove the `formatLastModified(modTime)` call from the CopyObject path.

## Phase 2 — Knowledge base
- [x] Append `.jules/bolt.md` entry documenting the AppendFormat + stack-buffer
      pattern (Rule 44 append-only).

## Phase 3 — Verification
- [x] `go build ./...` clean.
- [x] Standard test suite passes (including S3 copy tests).
- [x] Micro-benchmark confirms allocation reduction on the S3 Copy path.
- [x] PR carries `Resolves #968`; reviewer ✅ gate satisfied (Rule 55).