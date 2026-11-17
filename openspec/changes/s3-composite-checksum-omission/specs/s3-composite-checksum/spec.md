# Composite Checksum Omission

## Purpose

Never return a checksum that cannot describe the object being served.

## Requirements

### Requirement: Composite Checksums Are Not Returned

A GET or HEAD response SHALL NOT carry a composite (multipart) checksum, which is
recognisable by its `-<partCount>` suffix.

#### Scenario: Multipart download in checksum mode

- **WHEN** a client downloads a multipart object with
  `x-amz-checksum-mode: ENABLED`
- **THEN** the response carries a checksum computed over the stored bytes, never
  the stored composite value, so client-side validation succeeds

#### Scenario: Multipart download without checksum mode

- **WHEN** a client downloads a multipart object without checksum mode
- **THEN** the response carries no `x-amz-checksum-*` header at all

### Requirement: Whole-Object Checksums Still Returned

A checksum that describes the whole object SHALL still be returned when the
client asks for it.

#### Scenario: Single-part object in checksum mode

- **WHEN** a client downloads an object uploaded with a whole-object checksum
  and asks for checksum mode
- **THEN** the stored checksum is returned unchanged
