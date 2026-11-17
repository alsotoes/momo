# S3 Unsigned PUT

## Purpose

Accept a plain S3 `PUT` that declares no content hash, deriving the object hash
on the server so integrity is still verified, and answer external clients with
exactly one standard S3 response.

## Requirements

### Requirement: Derive The Hash For An Unsigned PUT

The gateway SHALL derive the object SHA-256 from the received bytes when an
external S3 client supplies no `X-Amz-Content-Sha256` or `Content-Sha256`
header.

#### Scenario: Unsigned upload is stored

- **WHEN** an external client sends `PUT /bucket/key` with a body and no content
  hash header
- **THEN** the gateway spools the body, computes its SHA-256, replays it through
  the shared ingest path, stores the object under that hash, and the stored
  digest is verified exactly as for a signed upload

#### Scenario: Oversized declared length

- **WHEN** the declared `Content-Length` exceeds the maximum object size
- **THEN** the gateway answers `413 EntityTooLarge` without reading the body

#### Scenario: Short body

- **WHEN** fewer bytes arrive than the declared `Content-Length`
- **THEN** the gateway answers `400 IncompleteBody` and stores nothing

### Requirement: One Standard Response For External Clients

The gateway SHALL NOT send interim momo framing or the momo ACK frame to an
external S3 client.

#### Scenario: Successful upload response

- **WHEN** an external client's upload completes
- **THEN** it receives a single `200 OK` carrying `ETag` (the content hash), and
  no `X-Momo-Metadata-Status` or `X-Momo-Replication-Mode` header

#### Scenario: Momo-framed clients are unaffected

- **WHEN** the client is a momo peer or a momo client that sent
  `X-Momo-Requested-Mode`
- **THEN** the interim framing and ACK behaviour is unchanged

### Requirement: Integrity Is Never Weakened

The gateway SHALL still refuse a supplied hash that does not match the stored
bytes, and SHALL still reject hash values containing path-traversal characters.

#### Scenario: Mismatched supplied hash

- **WHEN** a client supplies a content hash that does not match the received
  bytes
- **THEN** the object is deleted and the client receives `400 BadDigest`
