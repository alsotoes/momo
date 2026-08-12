# Change: Signed Payload Outbound SigV4 + Honest SSE Negotiation
**Related Issues:**
- https://github.com/alsotoes/momo/issues/776

## Why
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

## What Changes
- **`S3BlobStore.PutBlob` switches to `SIGNED_PAYLOAD`.** The body is spooled
  to a bounded temp file while its SHA-256 is computed, then uploaded with a
  real `Content-Length` and `X-Amz-Content-Sha256: <sha256(content)>`. The
  SigV4 signature now binds the content. Oversized blobs are rejected before
  upload (`EFBIG`), and the spool file is always removed. Memory stays
  bounded (disk spool, matching the aws-chunked spill pattern).
- **Inbound `UNSIGNED-PAYLOAD` and unsigned streaming are preserved.**
  Presigned uploads (aws-cli/boto3) still use the `UNSIGNED-PAYLOAD` literal
  on the gateway side, and `STREAMING-UNSIGNED-PAYLOAD-TRAILER` bodies are
  still de-framed without per-chunk verification (issue #773 posture). These
  inbound paths keep working; only momo's outbound signing becomes
  content-bound.
- **The gateway negotiates SSE honestly.** PUTs carrying
  `x-amz-server-side-encryption: AES256` are accepted: the header is captured
  via the standard S3 header list, persisted at rest with the object, and
  echoed on GET/HEAD — matching the request. SSE-C customer-key headers
  (`x-amz-server-side-encryption-customer-*`) are rejected with
  `400 InvalidRequest` and the customer key is never stored. SSE-KMS
  (`aws:kms` or `x-amz-server-side-encryption-aws-kms-key-id`) is rejected
  with `501 NotImplemented`. Unknown algorithms get `400 InvalidArgument`.
- **`x-amz-sdk-checksum-algorithm` is accepted and documented** (aws-cli v2
  sends it by default; rejecting it would break real clients). momo does not
  compute AWS additive checksums — integrity is content-addressed SHA-256 plus
  AEAD at rest — and this is documented in `docs/PROTOCOL.md`.

## Non-Goals
- No KMS integration, no customer-provided-key support, no AWS additive
  checksum computation.
- No change to the momo wire protocol between peers.
