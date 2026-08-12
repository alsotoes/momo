> GitHub Issue URL: https://github.com/alsotoes/momo/issues/776

# Signed Payload Outbound SigV4 + Honest SSE Negotiation Specification

## Purpose
This specification closes two integrity/security gaps on the S3 boundary.
First, momo's outbound S3 uploads (`S3BlobStore.PutBlob`) switch from the SigV4
`UNSIGNED-PAYLOAD` literal to `SIGNED_PAYLOAD`, so the request signature binds
the actual body content and tampered bodies fail signature verification at the
endpoint. Second, the S3 gateway stops silently downgrading SSE requests: it
accepts and persists `AES256`, echoes it on GET/HEAD, rejects SSE-C and
SSE-KMS with clear S3 errors, and documents its posture for
`x-amz-sdk-checksum-algorithm`.

## ADDED Requirements

### Requirement: Content-bound outbound SigV4 signing
The S3 blob store SHALL sign outbound PUT uploads with the `SIGNED_PAYLOAD`
payload hash (`X-Amz-Content-Sha256` = the hex SHA-256 of the exact body
bytes), so the SigV4 signature cryptographically binds the content. The body
SHALL be spooled to a bounded temp file while hashing (no full-body memory
buffering), uploaded with a real `Content-Length`, and the spool SHALL be
closed and removed on every path. Blobs exceeding the maximum object size SHALL
be rejected with `EFBIG` before any upload.

#### Scenario: Signed upload accepted by a verifying endpoint
- **GIVEN** an S3-compatible endpoint that verifies SigV4 over the received body
- **WHEN** momo uploads a blob via `PutBlob`
- **THEN** the declared `X-Amz-Content-Sha256` equals the actual body SHA-256 and the request signature verifies

#### Scenario: Tampered body fails verification
- **WHEN** a request signed over body A is sent with body B substituted
- **THEN** the endpoint rejects it (declared hash != actual body hash) because the signature no longer covers the content

#### Scenario: Oversized blob rejected before upload
- **GIVEN** content larger than `common.MaxFileSize`
- **WHEN** `PutBlob` is called
- **THEN** it returns `EFBIG` without issuing the upload

### Requirement: Inbound UNSIGNED tolerance preserved
The gateway SHALL continue accepting the `UNSIGNED-PAYLOAD` literal for
presigned uploads (aws-cli/boto3 compatibility) and SHALL continue de-framing
`STREAMING-UNSIGNED-PAYLOAD-TRAILER` aws-chunked bodies without per-chunk
signature verification (issue #773 posture). Only momo's **outbound** signing
becomes content-bound.

#### Scenario: Presigned upload still accepted
- **WHEN** a presigned PUT carries `X-Amz-Content-Sha256: UNSIGNED-PAYLOAD`
- **THEN** the gateway verifies the signature over the literal and stores the object

#### Scenario: Unsigned streaming PUT still de-framed
- **WHEN** a PUT carries `X-Amz-Content-Sha256: STREAMING-UNSIGNED-PAYLOAD-TRAILER` and `Content-Encoding: aws-chunked`
- **THEN** the gateway de-frames the chunks, consumes the trailer block, and stores the raw decoded bytes

### Requirement: Honest SSE negotiation
On PUT, the gateway SHALL evaluate the server-side-encryption headers:
- `x-amz-server-side-encryption: AES256` SHALL be accepted, captured as S3
  object metadata, persisted at rest, and echoed on GET/HEAD.
- Any `x-amz-server-side-encryption-customer-*` header (SSE-C) SHALL be
  rejected with `400 InvalidRequest`; the customer key SHALL never be stored.
- `x-amz-server-side-encryption: aws:kms` or
  `x-amz-server-side-encryption-aws-kms-key-id` (SSE-KMS) SHALL be rejected
  with `501 NotImplemented`.
- Any other encryption algorithm value SHALL be rejected with
  `400 InvalidArgument`.
The rejected SSE-C/KMS/unknown cases SHALL be rejected for every PUT variant
(object upload, CopyObject, CreateBucket) before any body processing.

#### Scenario: AES256 accepted and echoed
- **WHEN** a client PUTs an object with `x-amz-server-side-encryption: AES256`
- **THEN** the object is stored with that header and GET/HEAD echo `x-amz-server-side-encryption: AES256`

#### Scenario: SSE-C rejected
- **WHEN** a client PUTs with any `x-amz-server-side-encryption-customer-*` header
- **THEN** the gateway responds `400 InvalidRequest` and stores nothing

#### Scenario: SSE-KMS rejected
- **WHEN** a client PUTs with `x-amz-server-side-encryption: aws:kms`
- **THEN** the gateway responds `501 NotImplemented`

### Requirement: Checksum-algorithm header posture
The gateway SHALL accept `x-amz-sdk-checksum-algorithm` on PUT without error
and SHALL NOT claim to compute AWS additive checksums; object integrity is
provided by content-addressed SHA-256 and AEAD at rest. The posture is
documented in `docs/PROTOCOL.md`.

#### Scenario: aws-cli default upload succeeds
- **WHEN** aws-cli v2 PUTs an object with `x-amz-sdk-checksum-algorithm: CRC32`
- **THEN** the upload succeeds (no rejection); no AWS checksum is returned

## Requirement: No customer key retention
The system SHALL never persist, log, or echo a customer-provided encryption
key or its MD5 from SSE-C headers, including in error responses.
