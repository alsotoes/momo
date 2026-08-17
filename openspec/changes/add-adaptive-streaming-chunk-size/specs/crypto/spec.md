# Adaptive Streaming Cipher Chunk Size (StreamVersion v4)

## Purpose

This specification makes Momo's streaming AEAD chunk size configurable and
bounded by introducing a self-describing `StreamVersion v4` stream header that
carries the plaintext chunk size, while preserving decode of prior `v3` and `v2`
streams.

## ADDED Requirements

### Requirement: v4 Self-Describing Stream Format

`EncryptStream` SHALL emit `StreamVersion = 4` with a 3-byte header
`[version=4][chunkSizeHi][chunkSizeLo]` encoding the plaintext chunk size
(big-endian), followed by the 8-byte random seed. Framing after the header
(per-chunk length prefix, AEAD seal, integrity footer) SHALL be identical to v3.

#### Scenario: default-sized v4 stream
- **GIVEN** a cipher with the default chunk size
- **WHEN** `EncryptStream` encrypts a plaintext
- **THEN** the output begins with header bytes `0x04` then the big-endian
  2-byte default chunk size, and decrypts correctly.

#### Scenario: non-default-sized v4 stream decodes
- **GIVEN** a cipher configured with a non-default chunk size via
  `SetStreamChunkSize`
- **WHEN** `EncryptStream` then `DecryptStream` run on the same key
- **THEN** the plaintext round-trips exactly and the header reports the
  configured chunk size.

### Requirement: Bounded, Validated Chunk Size

The chunk size SHALL be bounded by `MinChunkSize` and `MaxChunkSize`.
`SetStreamChunkSize(n)` SHALL reject sizes outside that range (Rule 32). The v4
decoder SHALL validate the header chunk size within bounds before allocating
buffers.

#### Scenario: out-of-range chunk size is rejected
- **GIVEN** an attempt to set a chunk size below `MinChunkSize` or above
  `MaxChunkSize`
- **WHEN** `SetStreamChunkSize` is called
- **THEN** it returns an `EINVAL`-mapped error and the cipher retains its prior
  chunk size.

#### Scenario: decoder rejects a header chunk size out of bounds
- **GIVEN** a v4 stream whose header declares a chunk size outside
  `[MinChunkSize, MaxChunkSize]`
- **WHEN** `DecryptStream` reads the header
- **THEN** it returns `ErrStreamFormat` and allocates no oversized buffer.

### Requirement: Legacy Decode Preserved

`DecryptStream` SHALL continue to decode `v3` and `v2` streams and SHALL
reject unknown versions with `ErrStreamFormat`.

#### Scenario: v3 blob still decodes
- **GIVEN** a `StreamVersion = 3` stream (fixed 4096 chunks, footer)
- **WHEN** `DecryptStream` runs
- **THEN** it decodes correctly using the existing v3 path.

#### Scenario: v2 blob still decodes
- **GIVEN** a `StreamVersion = 2` legacy stream (no integrity footer)
- **WHEN** `DecryptStream` runs
- **THEN** it decodes correctly via the legacy path.

#### Scenario: unknown version rejected
- **GIVEN** a stream whose version byte is neither 4, 3, nor 2
- **WHEN** `DecryptStream` runs
- **THEN** it returns `ErrStreamFormat`.

### Requirement: Decoder Allocation is Bounded

The v4 decoder SHALL size its buffers from the validated header chunk size and
image capped by `MaxChunkSize`, never allocating more than `MaxChunkSize`
(Rule 4/32).

#### Scenario: allocation respects the bound
- **GIVEN** a validated v4 header chunk size
- **WHEN** the decoder allocates scratch buffers
- **THEN** allocations do not exceed `MaxChunkSize`.
