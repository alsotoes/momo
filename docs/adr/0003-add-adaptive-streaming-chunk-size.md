# 0003-add-adaptive-streaming-chunk-size

## Status
Accepted

## Confidence
High

## Context
Momo's streaming AEAD (`src/crypto/streaming.go`) encrypts/decrypts content in
a **fixed** `ChunkSize = 4096` byte plaintext chunks. The `EncryptStream` /
`DecryptStream` API takes no chunk-size parameter and the on-wire header is a
single `StreamVersion` byte. Consequences:

1. **Rigid throughput:** larger objects cannot take advantage of bigger
   plaintext chunks (fewer AEAD seals, lower per-chunk overhead), and memory
  -sensitive deployments cannot choose a smaller chunk.
2. **Non-self-describing:** the encoder's chunk size is invisible to the
   decoder; a stream written with a different chunk size cannot be validated up
   front against a bound, relying instead on the per-chunk length check.
3. Matching the original E2EE work, the format iterated version-to-version
   (`StreamVersion = 3`, legacy `2`); introducing a configurable chunk size
   warrants a new **`StreamVersion = 4`** so old blobs remain decodable.

## Decision
- v4 Self-Describing Stream Format: `EncryptStream` SHALL emit `StreamVersion = 4` with a 3-byte header `[version=4][chunkSizeHi][chunkSizeLo]` encoding the plaintext chunk size (big-endian), followed by the 8-byte random seed. Framing after the header (per-chunk length prefix, AEAD seal, integrity footer) SHALL be identical to v3.
- Bounded, Validated Chunk Size: The chunk size SHALL be bounded by `MinChunkSize` and `MaxChunkSize`. `SetStreamChunkSize(n)` SHALL reject sizes outside that range (Rule 32). The v4 decoder SHALL validate the header chunk size within bounds before allocating buffers.
- Legacy Decode Preserved: `DecryptStream` SHALL continue to decode `v3` and `v2` streams and SHALL reject unknown versions with `ErrStreamFormat`.
- Decoder Allocation is Bounded: The v4 decoder SHALL size its buffers from the validated header chunk size and image capped by `MaxChunkSize`, never allocating more than `MaxChunkSize` (Rule 4/32).

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/018-adaptive-scaling-peer-quality.md

## References
- Issue: #824
- PR: #833
- Spec: `openspec/changes/add-adaptive-streaming-chunk-size/`
- Blog: docs/blog/posts/018-adaptive-scaling-peer-quality.md

