# Change: S3 Integrity Checksums (x-amz-checksum-*)
**Related Issues:**
- https://github.com/alsotoes/momo/issues/820

## Why
Momо's S3 gateway (s3-tcp/s3-quic, `S3Communicator`) expects only the SHA-256
content hash as an integrity signal. SNIA S3 clients and integrity-verifying
tools ask for full `x-amz-checksum-*` support (CRC32, CRC32C, SHA-1, SHA-256) on
upload and download. Today these headers are ignored, so SDK checksum workflows
cannot be honored. S3 ALSO requires the server to verify a supplied digest at
write time and reject bad data with `400 BadDigest` — persisting an unverified
client checksum would be an integrity lie. This change implements the Tier P2
slice of issue #820.

## What Changes
- **Resolve + validate** `x-amz-checksum-algorithm` / `x-amz-checksum-<algo>`
  on PUT (`parseChecksum`). Uppercase→lowercase normalization; CRC32, CRC32C,
  SHA1, SHA256 supported; any other algorithm → deterministic `400
  InvalidRequest` (no silent fall-through).
- **Verify on EVERY write path.** The payload is teed through the algorithm's
  digest while streaming:
  - single-part plain PUT and aws-chunked: the digest feeds inside
    `S3Communicator.Read`; `getFile` finalizes and compares after `store.Put`
    (new optional `FinalizeS3Checksum` verifier). A mismatch deletes the object
    and returns `400 BadDigest` — nothing bad is persisted.
  - multipart: `CompleteMultipartUpload` hashes the assembled bytes, rejects
    `400 BadDigest` on mismatch *before* `store.Put`, and persists the checksum.
- **Persist + echo.** `collectS3Headers` captures `x-amz-checksum-*` and
  `x-amz-checksum-algorithm`, stored at rest via `PutS3Meta` and echoed on
  GET/HEAD by `appendS3MetaHeaders` (issue #772 machinery, unchanged).
- **GET checksum mode.** `GET` with `x-amz-checksum-mode: ENABLED` on an object
  without a persisted checksum computes a CRC32 over the stored bytes (bounded
  temp-file stream) and returns the `x-amz-checksum-crc32` header. Objects
  uploaded with a checksum already echo it.

## Non-Goals
- aws-chunked **trailer**-delivered checksums (`x-amz-trailer` + `...-SHA256`).
- Per-`UploadPart` checksum verification (SDK part-level checksums); final
  assembled-object checksum is verified instead.
- Algorithms beyond CRC32, CRC32C, SHA1, SHA256.
- Any change to the momo-native wire protocol between peers.

## Scope
This change implements only Tier P2 (integrity checksums) of issue #820. P1
(SSE breadth), P3 (versioning / bucket-config family), P4, P5 remain tracked by
issue #820.
