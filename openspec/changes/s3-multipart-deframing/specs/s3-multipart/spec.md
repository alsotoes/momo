# S3 Multipart Parts

## Purpose

Store exactly the bytes a client sent, without the transfer framing, and without
holding a part in memory.

## Requirements

### Requirement: De-frame Part Bodies

An `UploadPart` whose body carries aws-chunked framing SHALL be stored as its
decoded bytes.

#### Scenario: Chunked part with a trailer

- **WHEN** a client uploads a part with `Content-Encoding: aws-chunked` and a
  checksum trailer
- **THEN** the stored part equals the decoded payload and the assembled object
  size equals the sum of the decoded part sizes

#### Scenario: Plain part

- **WHEN** a client uploads a part with no framing
- **THEN** the stored part equals the request body

#### Scenario: Object size and checksum

- **WHEN** a multipart upload completes
- **THEN** the object length matches the uploaded file and a client's checksum
  validation succeeds

### Requirement: Parts Are Spill-backed

A part SHALL be written to disk rather than accumulated in memory.

#### Scenario: Large part

- **WHEN** a client uploads a part of several GiB
- **THEN** the daemon's heap does not grow with the part size, and assembly
  streams the spooled parts into the store

#### Scenario: Cleanup

- **WHEN** an upload completes or is aborted
- **THEN** its spooled part files are removed

### Requirement: No Whole-object Checksum On A Range

A `206 Partial Content` response SHALL NOT carry an `x-amz-checksum-*` header.

#### Scenario: Ranged download in checksum mode

- **WHEN** a client requests a byte range with
  `x-amz-checksum-mode: ENABLED`
- **THEN** the response carries no checksum header, so per-range validation
  cannot fail on a whole-object value
