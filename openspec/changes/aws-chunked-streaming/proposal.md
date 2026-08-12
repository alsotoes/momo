# Change: S3 aws-chunked Streaming Payload Support
**Related Issues:**
- https://github.com/alsotoes/momo/issues/773

## Why
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

## What Changes
- **Detect** streaming uploads in the S3 PUT path (`HandshakeServer`) via
  `Content-Encoding` containing `aws-chunked` and/or `X-Amz-Content-Sha256`
  being a `STREAMING-*` literal.
- **Add an `awsChunkedReader`** (new file `src/transport/aws_chunked.go`):
  an `io.Reader` that decodes the `aws-chunked` framing off the wire and
  streams the de-framed bytes out. It computes the decoded SHA-256 and decoded
  byte count as it reads, and, for the signed variant, verifies every chunk
  signature (chained HMAC) against the derived SigV4 signing key.
- **Resolve the content hash before the momo pipeline runs**: after decoding,
  set `m.meta.Hash = <decoded sha256 hex>` and
  `m.meta.Size = <decoded size>` (cross-checked against
  `X-Amz-Decoded-Content-Length`). De-framed bytes are spilled to a bounded
  temp file and served back through `S3Communicator.Read()`, so the existing
  server pipeline (dedup, CRUSH placement, `getFile` hash check, splay/chain
  replication) runs **unchanged** with the real content-addressed hash.
- **Keep `verifySigV4Signature` using the `STREAMING-*` literal** as the
  canonical payload hash — that is correct per the AWS spec and already passes
  for streaming requests; add a regression test to lock it in. Document that
  the per-chunk signatures (not the header signature) carry the streaming
  authenticity guarantee.
- **Handle `Expect: 100-continue`** on streaming PUTs (respond `100 Continue`
  before reading the body) and enforce size-based read deadlines to prevent
  slowloris/resource-exhaustion (Rule 35).
- **Error mapping**: bad chunk signature → `403 SignatureDoesNotMatch`;
  malformed framing / decoded-length mismatch → `400 InvalidArgument`;
  decoded size over `MaxFileSize` → `413 EntityTooLarge`.
- **Unsigned streaming** (`STREAMING-UNSIGNED-PAYLOAD-TRAILER`, and
  `aws-chunked` under `UNSIGNED-PAYLOAD`): de-frame and skip per-chunk
  verification, documenting the security posture; integrity is provided by the
  content-addressing hash computed during decode.
- **Docs**: update `docs/PROTOCOL.md` with the streaming wire format, the
  security posture, and why the outbound `S3BlobStore` may keep using
  `UNSIGNED-PAYLOAD`.

**Correction to the issue text:** a chunk's *data* is transmitted as raw bytes,
not hex-encoded. Only the chunk-size prefix (hex) and the `chunk-signature=`
value (hex) are textual. This matches the AWS SigV4 streaming spec, the
aws-java-sdk-v1 `AwsChunkedEncodingInputStream`, the aws-sdk-net
`ChunkedUploadWrapperStream`, and MinIO's `s3ChunkedReader`.

## Impact
- Affected specs: `streaming`
- Affected code: `src/transport/s3_communicator.go`, `src/transport/sigv4.go`,
  `src/transport/aws_chunked.go` (new), `docs/PROTOCOL.md`
- No changes to `src/server`, `src/storage`, or the momo wire protocol — the
  gateway decodes/de-frames at the transport boundary and the standard
  PUT/replication pipeline is preserved.