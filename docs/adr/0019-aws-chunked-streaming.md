# 0019-aws-chunked-streaming

## Status
Proposed

## Confidence
Medium

## Context
AWS SDK Go v2, aws-sdk-java, aws-sdk-net, and boto3 upload single objects with
**streaming payloads** in many configurations. They set
`X-Amz-Content-Sha256` to a `STREAMING-*` literal and frame the body in
`aws-chunked` format instead of sending the whole body as raw bytes. The current
gateway:

1. Treats the aws-chunked body as raw content, so `getFile` stores the chunk
   frames (hex sizes, `;chunk-signature=` extensions, CRLFs) as object bytes →
   **stored objects are corrupted**.
2. Uses the `STREAMING-*` literal as `meta.Hash`, so the `getFile` SHA-256
   comparison against a literal string always fails (`EBADMSG`) → **every
   streaming upload errors out after the handshake**.
3. Never verifies the per-chunk SigV4 signatures, so even if framing were
   stripped there would be no authenticity check on streamed bytes.

These are the default upload paths for the most widely used S3 SDKs, so the
gateway is effectively unusable for them.

## Decision
- Streaming Upload Detection: The system SHALL identify an S3 upload as a streaming (aws-chunked) upload when the PUT request's `Content-Encoding` header contains `aws-chunked` OR its `X-Amz-Content-Sha256` header is `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`, `STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER`, `STREAMING-UNSIGNED-PAYLOAD-TRAILER`, or any other `STREAMING-*` literal.
- aws-chunked De-framing: The system SHALL decode the `aws-chunked` body into its raw content. Each chunk on the wire is `<hex-size>[;chunk-signature=<64-hex>]\r\n` followed by the raw (binary) chunk data bytes, followed by `\r\n`. The stream ends with a `0`-size chunk (`0;chunk-signature=<sig>\r\n\r\n`), optionally followed by a trailer block (`<name>:<value>\r\n` lines terminated by a blank line) for the `*-TRAILER` variants. The chunk signature extension may be absent in unsigned mode. De-framing SHALL stream (bounded...
- Content Hash and Size Resolution: For a streaming upload, the system SHALL NOT use the `STREAMING-*` literal or the framed `Content-Length` as the momo payload metadata. It SHALL set `meta.Hash` to the SHA-256 hex of the decoded content and `meta.Size` to the decoded byte count. When `X-Amz-Decoded-Content-Length` is present it SHALL be cross-checked against the decoded byte count and a mismatch SHALL reject the upload. The de-framed content is then readable through `S3Communicator.Read()` so dedup (`store.Has`), CRUSH placement...
- Signed Chunk Verification (STREAMING-AWS4-HMAC-SHA256-PAYLOAD): For signed streaming, the system SHALL verify every chunk against the chained SigV4 algorithm, using the request signature as the seed. For each chunk, the signing key is `deriveSigningKey(secretKey, dateStamp, region)` and the chunk string-to-sign is: `"AWS4-HMAC-SHA256-PAYLOAD\n" + <amzDate> + "\n" + <dateStamp>/<region>/s3/aws4_request + "\n" + <previousChunkSignature> + "\n" + <emptyStringSHA256> + "\n" + <sha256hex(chunkData)>` where `<emptyStringSHA256>` is the SHA-256 of the empty string ...
- Unsigned Streaming (STREAMING-UNSIGNED-PAYLOAD-TRAILER and aws-chunked under UNSIGNED-PAYLOAD): The system SHALL accept unsigned streaming bodies, de-frame them, and skip per-chunk signature verification when the chunk signature field is empty or absent. The security posture SHALL be documented: without per-chunk signatures the stream itself is unauthenticated between the header signature check and storage; integrity is still provided by the content-addressing hash computed during decode (a wrong/hand-edited body simply produces a different content hash, which is stored under its own key �...
- DoS Bounds, Deadlines, and 100-continue: The system SHALL set a read deadline sized to the decoded content length (bounded, mirroring the server's size-based deadline) around the streaming body read and clear it afterwards. If the PUT carries an `Expect: 100-continue` header, the gateway SHALL send `HTTP/1.1 100 Continue\r\n\r\n` before reading the body. Chunk header lines SHALL be length-bounded, individual chunk size SHALL be capped (AWS maximum), and the total decoded size SHALL be capped at `common.MaxFileSize`.
- Spill Cleanup: The system SHALL clean up the decoded-content spill file (and any partial blob) on every exit path: success, signature failure, framing error, size overflow, and connection error.
- Regression Tests: The system SHALL ship regression tests covering: chunk de-framing parser (valid signed stream, corrupted signature, truncated/failed chunk, unsigned trailer variant); SigV4 canonical-signature verification with the `STREAMING-*` literal; the full end-to-end streaming PUT → GET round trip; decoded-length mismatch rejection; `Expect: 100-continue`; oversized upload rejection; and streaming PUT through splay/chain replication producing identical replicated content.
- Documentation: The system SHALL document in `docs/PROTOCOL.md`: the supported `aws-chunked` variants, the de-framing behavior at the gateway boundary, the chunk signature verification algorithm, the unsigned-streaming security posture, and a note explaining why the outbound `S3BlobStore` may continue to use `UNSIGNED-PAYLOAD` (its blob keys are momo content hashes and it is an opaque backend, so per-chunk upload signatures add no integrity value).

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/aws-chunked-streaming/
- Blog: docs/blog/posts/...md
