# Tasks — Bolt: S3 HTTP header time-format allocation removal

## Phase 1 — Implementation
- [x] Rewrite the three `Last-Modified` header renderings in
      `S3Communicator.HandshakeServer` (GET 200, GET 304, HEAD) using
      `time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)`.
- [x] Remove the now-unused `formatHTTPLastModified` helper.

## Phase 2 — Knowledge base
- [x] Append `.jules/bolt.md` entry documenting the AppendFormat + stack-buffer
      pattern (Rule 44 append-only).

## Phase 3 — Verification
- [x] `go build ./...` clean.
- [x] Standard test suite passes (including S3 GET/HEAD tests).
- [x] Header output byte-for-byte identical (existing S3 tests assert
      `Last-Modified` format).
- [x] PR carries `Resolves #977`; reviewer ✅ gate satisfied (Rule 55).