> GitHub Issue URL: https://github.com/alsotoes/momo/issues/903

# core-integrity-verification Specification

## Purpose
Make integrity verification a property of the **storage/ingest core**, not of any
surface protocol. Add a protocol-agnostic additive checksum contract to
`FileMetadata`, centralize verification so every ingress and replication hop
gets the same guarantee, and demote surfaces (s3-*, momo-tcp, momo-quic) to pure
adapters that map their client protocols onto the core contract. This removes
protocol lock-in and lets any future surface inherit integrity unchanged.

## ADDED Requirements

### Requirement: protocol-agnostic integrity contract
The system SHALL model additive integrity digests in `common.FileMetadata` as a
list of `ChecksumRef{Algo, Value}` alongside the authoritative SHA-256 `Hash`.
`Hash` SHALL remain the sole content-addressable identifier; `Checksums` are
additive and MUST NOT be independently addressable.

#### Scenario: carry a checksum additively
- **GIVEN** an object stored with an additive checksum and valid auth
- **WHEN** its metadata is read or forwarded to a peer
- **THEN** the SHA-256 `Hash` is unchanged and the checksum is preserved as an additive field/header

#### Scenario: additive-only wire change
- **GIVEN** a peer that does not support additive checksums
- **WHEN** it receives forwarded object metadata
- **THEN** it stores/echoes no checksum and continues to operate unaffected

### Requirement: centralized ingest verification
The system SHALL verify every supplied additive checksum in the shared ingest
path (`getFile`/store), independent of the ingress surface, and reject a
mismatch by not persisting the object.

#### Scenario: any-surface mismatch rejected
- **GIVEN** an upload (via an S3 or native transport) carrying a wrong additive checksum and valid auth
- **WHEN** the shared ingest path processes it
- **THEN** the upload is rejected (no object persisted)

#### Scenario: no-checksum inert
- **GIVEN** an upload with no additive checksum and valid auth
- **WHEN** the shared ingest path processes it
- **THEN** verification adds no work on the common path and the upload proceeds on SHA-256 `Hash` alone

### Requirement: replication hop re-verification
The system SHALL re-verify an additive checksum at every replication hop that
receives object bytes, so integrity holds end-to-end across a chain/splay fan-out.

#### Scenario: replica rejects tamper
- **GIVEN** a replication hop whose received bytes do not match the carried additive checksum
- **WHEN** the hop ingests the forwarded bytes
- **THEN** the hop rejects the write (object not persisted on that replica)

### Requirement: surface adapters
The system SHALL keep client-protocol specifics (e.g. S3 `x-amz-checksum-*`
header parse/arm/encode) inside surface adapters, mapping them onto the core
contract, not into shared logic.

#### Scenario: S3 adapter maps checksum
- **GIVEN** an S3 PUT with `x-amz-checksum-sha256` and valid auth
- **WHEN** the S3 adapter ingests it
- **THEN** it maps the header into the core `ChecksumRef` and the centralized verifier consumes it

## UNCHANGED Behavior
- SHA-256 `Hash` remains the content-address + dedup key; ETag unchanged.
- Fixed momo wire framing fields (`Name`/`Hash`/`Size`/`RemotePath`/`ModTime`)
  unchanged; checksum extras are additive only.
- aws-chunked trailing-checksum form (`x-amz-trailer`) still unsupported.
- Per-`UploadPart` per-part checksums still out of scope (assembled checksum only).
