# 0040-s3-integrity-checksums

## Status
Proposed

## Confidence
Low

## Context
Momо's S3 gateway (s3-tcp/s3-quic, `S3Communicator`) expects only the SHA-256
content hash as an integrity signal. SNIA S3 clients and integrity-verifying
tools ask for full `x-amz-checksum-*` support (CRC32, CRC32C, SHA-1, SHA-256) on
upload and download. Today these headers are ignored, so SDK checksum workflows
cannot be honored. S3 ALSO requires the server to verify a supplied digest at
write time and reject bad data with `400 BadDigest` — persisting an unverified
client checksum would be an integrity lie. This change implements the Tier P2
slice of issue #820.

## Decision
- Upload checksum acceptance & verification: The system SHALL accept `x-amz-checksum-<algo>` (CRC32, CRC32C, SHA1, SHA256) or `x-amz-checksum-algorithm` on PUT, compute the checksum over the bytes actually stored, and reject a mismatch with HTTP 400 `BadDigest` without persisting the object. This MUST hold for single-part plain PUT, aws-chunked, and multipart.
- GET checksum mode: The system SHALL, for GET with `x-amz-checksum-mode: ENABLED` on an object that carries no persisted checksum, compute a checksum over the stored bytes and return it in the `x-amz-checksum-<algo>` response header.
- deterministic rejection: The system SHALL return HTTP 400 `InvalidRequest` for any unsupported checksum algorithm and MUST NOT fall through to another handler. ## UNCHANGED Behavior - ETag remains the SHA-256 content hash (S3-compatible gateways need not use `MD5(partETags)`). - No change to the momo-native wire protocol; checksum is an S3-surface feature. - aws-chunked trailer-delivered checksums remain unsupported (documented).

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/s3-integrity-checksums/
- Blog: docs/blog/posts/...md
