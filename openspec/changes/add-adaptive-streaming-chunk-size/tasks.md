# Adaptive Streaming Cipher Chunk Size (StreamVersion v4)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/824

## A: v4 format + configurable chunk size

- [x] A1. Add `ChunkSize`, `MinChunkSize`, `MaxChunkSize` constants and
  `StreamVersion = 4` (`src/crypto/streaming.go`); document the v4 header
  layout.
- [x] A2. Add a chunk-size field on `Cipher` (default `ChunkSize`) plus
  `SetStreamChunkSize(n int) error` that validates `[Min, Max]` and maps
  out-of-range to `EINVAL`.
- [x] A3. Rewrite `EncryptStream` to emit the 3-byte v4 header
  `[4][hi][lo]` + seed, using the cipher's chunk size; keep footer/truncation
  semantics.
- [x] A4. Add `decryptStreamV4` reading the 3-byte header, validating the
  chunk size, and sizing buffers by `Min(declared, MaxChunkSize)`; keep v3 and
  v2 decoders in `DecryptStream`.

## B: Tests

- [x] B1. Round-trips: default 4096 and a non-default (valid) chunk size.
- [x] B2. Rejection: `SetStreamChunkSize` out-of-range (min/max) leaves prior
  size; v4 header out-of-bounds size → `ErrStreamFormat`.
- [x] B3. Legacy: v3 and v2 streams still decode; unknown version rejected.
- [x] B4. Carried-over v4 tamper / truncation / footer-count-contract tests.

## C: Docs, Benchmarks, Verification

- [x] C1. Docs parity (Rule 27): `docs/PROTOCOL.md` (v4 header layout),
  `docs/CONFIGURATION.md` (if exposed as config) or note default; ensure v3/v2
  legacy documented.
- [x] C2. Benchmarks (Rule 34): default-size v4 path; note in
  `docs/PERFORMANCE.md`.
- [x] C3. `go build ./...`, `go test -race ./...` (incl. crypto),
  `go vet ./...`, `gofmt`.

## Steering-Rule Compliance Notes

- **Rules 4/32:** chunk size bounded; decoder alloc capped at `MaxChunkSize`.
- **Rules 5:** unit tests for all new behavior and carried-over integrity
  checks.
- **Rule 7:** versioned, documented wire-format change.
- **Rule 33:** streaming is transport-agnostic, applied across all protocols.
- **Rule 38:** v2/v3 legacy versions preserved and dispatched.
