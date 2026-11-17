# S3 Checksum-Mode Response Headers

## Purpose

Return checksum headers only when the client asks for them, as S3 does, so a
client never validates a checksum it did not request.

## Requirements

### Requirement: Checksums Only In Checksum Mode

A GET or HEAD response SHALL carry `x-amz-checksum-*` only when the request
includes `x-amz-checksum-mode: ENABLED`.

#### Scenario: Plain download carries no checksum

- **WHEN** a client downloads an object without asking for checksum mode
- **THEN** the response has no `x-amz-checksum-*` header, so the client performs
  no checksum validation

#### Scenario: Multipart object downloads cleanly

- **WHEN** an object uploaded through the multipart API is downloaded without
  checksum mode
- **THEN** the client receives the bytes without a composite-checksum mismatch

#### Scenario: Checksum mode still answers

- **WHEN** a client sends `x-amz-checksum-mode: ENABLED`
- **THEN** the response carries the stored checksum, or one computed over the
  stored bytes when the object never had one

### Requirement: Other Metadata Is Unaffected

Preserved object metadata (Content-Type, `x-amz-meta-*`, cache/encoding headers)
SHALL continue to be echoed on every response.

#### Scenario: Metadata still echoed

- **WHEN** an object with `x-amz-meta-*` is downloaded
- **THEN** those headers are present regardless of checksum mode
