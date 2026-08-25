> GitHub Issue URL: https://github.com/alsotoes/momo/issues/820

# s3-integrity-checksums Specification

## Purpose
Honor the SNIA S3 object-integrity contract: accept, verify, persist and echo
`x-amz-checksum-*` on upload; compute and return a checksum on GET with
`x-amz-checksum-mode: ENABLED`; deterministic rejection for unsupported
algorithms. Applies to both `s3-tcp` and `s3-quic` (shared `S3Communicator`).
This is the Tier P2 slice of issue #820.

## ADDED Requirements

### Requirement: Upload checksum acceptance & verification
The system SHALL accept `x-amz-checksum-<algo>` (CRC32, CRC32C, SHA1, SHA256) or
`x-amz-checksum-algorithm` on PUT, compute the checksum over the bytes actually
stored, and reject a mismatch with HTTP 400 `BadDigest` without persisting the
object. This MUST hold for single-part plain PUT, aws-chunked, and multipart.

#### Scenario: correct checksum accepted
- **GIVEN** a client PUTs an object with `X-Amz-Checksum-Sha256` matching the body and valid auth
- **WHEN** the gateway processes the upload
- **THEN** the object is stored and the checksum is echoed on subsequent HEAD/GET

#### Scenario: bad checksum rejected before persist
- **GIVEN** a client PUTs an object whose `X-Amz-Checksum-Sha256` does not match the body and valid auth
- **WHEN** the gateway processes the upload
- **THEN** it returns HTTP 400 `BadDigest` and does NOT store the object

#### Scenario: multipart bad checksum rejected before complete
- **GIVEN** a client completes a multipart upload whose assembled bytes do not match the supplied `X-Amz-Checksum-Sha1`
- **WHEN** the gateway processes `CompleteMultipartUpload`
- **THEN** it returns HTTP 400 `BadDigest` and does NOT call `store.Put`

### Requirement: GET checksum mode
The system SHALL, for GET with `x-amz-checksum-mode: ENABLED` on an object that
carries no persisted checksum, compute a checksum over the stored bytes and
return it in the `x-amz-checksum-<algo>` response header.

#### Scenario: checksum returned on enabled GET
- **GIVEN** a GET on an existing object with `x-amz-checksum-mode: ENABLED` and valid auth
- **WHEN** the object has no persisted checksum
- **THEN** the response includes a computed `x-amz-checksum-<algo>` header

#### Scenario: persisted checksum echoed
- **GIVEN** a GET or HEAD on an object uploaded with a checksum and valid auth
- **WHEN** the object carries a persisted checksum
- **THEN** the response includes the persisted `x-amz-checksum-<algo>` header

### Requirement: deterministic rejection
The system SHALL return HTTP 400 `InvalidRequest` for any unsupported checksum
algorithm and MUST NOT fall through to another handler.

#### Scenario: unsupported algorithm
- **GIVEN** a PUT with an unknown `x-amz-checksum-algorithm`
- **WHEN** the gateway processes the request
- **THEN** it returns HTTP 400 `InvalidRequest` with a clear message

## UNCHANGED Behavior
- ETag remains the SHA-256 content hash (S3-compatible gateways need not use
  `MD5(partETags)`).
- No change to the momo-native wire protocol; checksum is an S3-surface feature.
- aws-chunked trailer-delivered checksums remain unsupported (documented).
