# 0003-add-adaptive-streaming-chunk-size

## Status
Accepted

## Confidence
High

## Context


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
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-adaptive-streaming-chunk-size/
- Blog: docs/blog/posts/...md
