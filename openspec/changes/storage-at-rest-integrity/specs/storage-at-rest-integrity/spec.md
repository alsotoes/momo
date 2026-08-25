> GitHub Issue URL: https://github.com/alsotoes/momo/issues/924

# storage-at-rest-integrity Specification

## Purpose
Detect and refuse to serve blobs whose at-rest bytes no longer match their
SHA-256 content-address key. Two mechanisms: verify-on-read (the read path
re-derives the hash and fails instead of serving corrupt bytes) and a
background scrub that iterates referenced blobs, re-hashes each, and
quarantines corrupt ones so later reads fail explicitly with `ENOENT`.
Bounded-memory, defensive (panic-recovered), cancellable on store close.

## ADDED Requirements

### Requirement: Verify-on-read failures are explicit
The system SHALL, when `verify_on_read` is enabled (default `true`), recompute
the SHA-256 over the entire blob stream returned by `CASStore.Get` and, at EOF,
assert it equals the blob's content-hash key. When it does not equal, reads
SHALL fail with an error wrapping `common.ErrIntegrityMismatch` and
`syscall.EBADMSG`; no corrupt bytes are served.

#### Scenario: valid blob serves normally
- **GIVEN** a stored blob whose bytes hash to its key
- **WHEN** a caller reads the stream to EOF
- **THEN** the read completes with `io.EOF` and no integrity error

#### Scenario: corrupted blob fails at EOF
- **GIVEN** a stored blob whose bytes have been modified so they no longer hash
  to its key
- **WHEN** a caller reads the stream to EOF
- **THEN** the read returns an error wrapping `common.ErrIntegrityMismatch` and
  `syscall.EBADMSG`

### Requirement: Verify-on-read is configurable
The system SHALL accept a `[storage]` config key `verify_on_read` (boolean,
default `true`) controlling whether `CASStore.Get` wraps blob streams with
content-hash verification.

#### Scenario: disabled verify-on-read
- **GIVEN** `verify_on_read = false`
- **WHEN** a caller reads a corrupted blob to EOF
- **THEN** the read completes without an integrity error

### Requirement: Background scrub quarantines corrupt blobs
The system SHALL provide `CASStore.StartScrub` (call-safe at most once,
mirroring `StartGC`) that periodically iterates referenced blobs in the
`objects` bucket, re-reads and re-hashes each via `BlobStore.GetBlob`, and for
any blob whose recomputed hash does not equal its key, quarantines it: removes
the blob content and its object metadata so subsequent reads for names mapping
to that hash fail with `syscall.ENOENT`. The pass SHALL be bounded-memory,
panic-recovered, cancellable on store close, and SHALL NOT hold the store read
lock across blob I/O.

#### Scenario: scrub round-trips a healthy store
- **GIVEN** a store whose blobs all hash to their keys
- **WHEN** a scrub pass runs
- **THEN** no blobs are quarantined and all reads still succeed

#### Scenario: scrub quarantines a corrupted blob
- **GIVEN** a stored blob whose bytes have been modified to no longer hash to
  their key
- **WHEN** a scrub pass runs
- **THEN** the blob content and metadata are removed, and a subsequent read of a
  name mapping to that hash fails with `syscall.ENOENT`

### Requirement: Scrub interval is configurable
The system SHALL accept a `[storage]` config key `scrub_interval` (integer
seconds, default `3600`) controlling how often the scrub pass runs.

#### Scenario: default interval
- **GIVEN** no `scrub_interval` configured
- **THEN** the default is `3600` seconds

### Requirement: Content-hash streaming helper
The system SHALL expose `common.HashReader(r io.Reader)` returning the hex
SHA-256 of `r` while streaming through a fixed-size buffer (bounded memory),
mirroring `common.HashFile`.

#### Scenario: HashReader matches HashFile
- **GIVEN** the same content
- **WHEN** hashed once via `common.HashReader` and once via `common.HashBytes`
- **THEN** both report the same hex digest
