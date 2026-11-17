# S3 Bucket-Relative Object Names

## Purpose

Make a path-style S3 PUT resolve to the same object every read path resolves, so
a standard S3 client can round-trip data through the gateway.

## Requirements

### Requirement: Store The Bucket-Relative Key

The gateway SHALL store an uploaded object under the bucket-relative key when
the request path carries a bucket segment.

#### Scenario: Path-style PUT round-trips

- **WHEN** a client sends `PUT /bucket/dir/key.txt`
- **THEN** the object is stored as `dir/key.txt`, so `GET`, `HEAD`, `DELETE`
  and `COPY` of the same path resolve it

#### Scenario: Top-level path keeps flat semantics

- **WHEN** a client sends `PUT /file.txt` (no bucket segment)
- **THEN** the object is stored as `file.txt`, preserving legacy flat mode

### Requirement: Explicit Parse State

The gateway SHALL track "metadata already parsed" with explicit state rather
than inferring it from an empty object name.

#### Scenario: Empty key is not mistaken for "not parsed"

- **WHEN** a request parses to an empty key
- **THEN** `ReceiveMetadata` returns the parsed metadata instead of blocking for
  another request on the connection

### Requirement: One Naming Rule

Every parse path SHALL apply the same naming rule, including the
`OPTIONS`-then-`PUT` fallback.

#### Scenario: Fallback parse agrees with the primary parse

- **WHEN** the metadata is parsed from a follow-up request rather than the initial
  one
- **THEN** the stored name is the same bucket-relative key the primary parse
  would produce
