# 0050-signed-payload-and-sse

## Status
Proposed

## Confidence
Medium

## Context
Two integrity/security gaps in the S3 boundary:

1. **Outbound uploads are not content-bound.** `S3BlobStore.PutBlob` signs
   every PUT with the SigV4 `UNSIGNED-PAYLOAD` literal, so the signature
   covers the request headers but **not** the body. A tampered body (or a
   man-in-the-middle rewriting the bytes) would still pass signature
   verification against a compliant S3 endpoint, because the signed payload
   hash is the literal, not the content. MinIO/AWS verify SIGNED_PAYLOAD
   uploads by recomputing the body hash server-side.
2. **Inbound SSE requests are silently downgraded.** The gateway ignores
   `x-amz-server-side-encryption`, `x-amz-server-side-encryption-customer-*`,
   and `x-amz-sdk-checksum-algorithm`. A client that requests AES256, SSE-C,
   or SSE-KMS gets no error and no guarantee — it believes its objects are
   encrypted with the requested scheme when momo actually encrypts at rest
   with its own AES-256-GCM envelope. Accepting a customer-provided key or a
   KMS contract momo cannot honor and then silently ignoring it is worse than
   a clear rejection.

## Decision
- Content-bound outbound SigV4 signing: The S3 blob store SHALL sign outbound PUT uploads with the `SIGNED_PAYLOAD` payload hash (`X-Amz-Content-Sha256` = the hex SHA-256 of the exact body bytes), so the SigV4 signature cryptographically binds the content. The body SHALL be spooled to a bounded temp file while hashing (no full-body memory buffering), uploaded with a real `Content-Length`, and the spool SHALL be closed and removed on every path. Blobs exceeding the maximum object size SHALL be rejected with `EFBIG` before any upload.
- Inbound UNSIGNED tolerance preserved: The gateway SHALL continue accepting the `UNSIGNED-PAYLOAD` literal for presigned uploads (aws-cli/boto3 compatibility) and SHALL continue de-framing `STREAMING-UNSIGNED-PAYLOAD-TRAILER` aws-chunked bodies without per-chunk signature verification (issue #773 posture). Only momo's **outbound** signing becomes content-bound.
- Honest SSE negotiation: On PUT, the gateway SHALL evaluate the server-side-encryption headers: - `x-amz-server-side-encryption: AES256` SHALL be accepted, captured as S3 object metadata, persisted at rest, and echoed on GET/HEAD. - Any `x-amz-server-side-encryption-customer-*` header (SSE-C) SHALL be rejected with `400 InvalidRequest`; the customer key SHALL never be stored. - `x-amz-server-side-encryption: aws:kms` or `x-amz-server-side-encryption-aws-kms-key-id` (SSE-KMS) SHALL be rejected with `501 NotImplemented`. ...
- Checksum-algorithm header posture: The gateway SHALL accept `x-amz-sdk-checksum-algorithm` on PUT without error and SHALL NOT claim to compute AWS additive checksums; object integrity is provided by content-addressed SHA-256 and AEAD at rest. The posture is documented in `docs/PROTOCOL.md`. ## Requirement: No customer key retention The system SHALL never persist, log, or echo a customer-provided encryption key or its MD5 from SSE-C headers, including in error responses.

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
- Spec: openspec/changes/signed-payload-and-sse/
- Blog: docs/blog/posts/...md
