# aws-chunked Trailing Checksums

## Purpose

Make a client's trailing checksum an enforced integrity assertion rather than a
decoration.

## Requirements

### Requirement: Verify A Trailing Checksum

A trailing `x-amz-checksum-<algo>` SHALL be verified over the decoded bytes before
the payload is stored.

#### Scenario: Matching checksum

- **WHEN** a client streams an upload with a trailer whose checksum matches the
  decoded bytes
- **THEN** the upload is accepted

#### Scenario: Mismatch

- **WHEN** the trailing checksum does not match the decoded bytes
- **THEN** the request is refused with `400 BadDigest` and no object is stored

#### Scenario: CRC-64/NVME

- **WHEN** a client sends its default trailer checksum, CRC-64/NVME
- **THEN** the value is verified like any other algorithm

#### Scenario: Unsupported algorithm

- **WHEN** the trailer names a checksum algorithm the gateway does not support
- **THEN** the request is refused with `400 InvalidRequest`

### Requirement: Bounded And Strict Parsing

The trailing-header block SHALL be bounded and validated so it cannot be used to
inject bytes.

#### Scenario: Malformed trailer

- **WHEN** a trailer line has no colon, a name outside the header-name charset,
  or the block exceeds 64 KiB or 16 entries
- **THEN** the request is refused with `400 InvalidArgument`

#### Scenario: Declared but absent

- **WHEN** the client declares `X-Amz-Trailer` but sends no such trailer
- **THEN** the upload proceeds without checksum verification, because there is
  nothing to verify

### Requirement: Parts Are Covered

A multipart part body SHALL receive the same verification.

#### Scenario: Chunked part with a trailer

- **WHEN** a part is uploaded with aws-chunked framing and a trailing checksum
- **THEN** the trailer is verified before the part is accepted
