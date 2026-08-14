# Proposal: Adaptive Streaming Cipher Chunk Size (StreamVersion v4)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/824

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`

## 1. Problem

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

## 2. Proposed Solution

Introduce `StreamVersion = 4`, a self-describing stream header that carries a
**bounded, validated** plaintext chunk size, while keeping `v2` and `v3`
decode paths intact for backward compatibility.

### 2.1 Wire format (v4)

Header becomes `[version=4][chunkSizeHi][chunkSizeLo]` (3 bytes), then the
existing 8-byte random seed. `chunkSize` is the plaintext chunk length, big-endian
2 bytes. The rest of the framing (per-chunk length prefix, AEAD seal, integrity
footer) is unchanged from v3.

### 2.2 A configurable, bounded chunk size

- Keep `ChunkSize = 4096` as the **default** so all existing call sites behave
  identically when no explicit size is set.
- Add a minimum `MinChunkSize` and maximum `MaxChunkSize` (Rule 32 — bounded
  allocation). `SetStreamChunkSize(n)` validates `MinChunkSize <= n <=
  MaxChunkSize` and rejects out-of-range values with `EINVAL` (or a domain
  error unwrapping to `EINVAL`), so a malicious/typo chunk size cannot cause
  unbounded buffer allocation.
- `EncryptStream` uses the cipher's configured chunk size (defaults to
  `ChunkSize`). Decryption is size-agnostic: it reads the declared chunk size
  from the v4 header, validates it within bounds, and sizes its buffers
  accordingly (still capped by `MaxChunkSize`).

### 2.3 Backward compatibility

- `DecryptStream` dispatches on version: `4` → v4 decoder; `3` →
  existing v3 decoder; `2` → existing legacy decoder; anything else →
  `ErrStreamFormat` (Rule 38 compatibility).
- A config that never calls `SetStreamChunkSize` produces v4 streams with the
  default 4096 chunk — wire-identical framing to v3 except the extra 2 header
  bytes.

## 3. Wire & Protocol Impact

- **New v4 header** adds 2 bytes (chunk-size field) over v3. This is an
  intentional, versioned format change (Rule 7 — documented in PROTOCOL.md).
- **All existing v3 and v2 blobs remain readable.** No legacy ciphertext is
  orphaned.
- `MaxChunkSize` currently also bounds the per-chunk length check; it is
  redefined as the maximum allowed plaintext chunk + framing, keeping the
  decoder's allocation bound explicit (Rule 4/32).

## 4. Testing

- Unit: v4 round-trip at default and at a non-default chunk size; set chunk
  size with default no-op; out-of-range size rejected (min/max); v3 and v2
  legacy streams still decode; unsupported versions still rejected.
- Truncation / footer-count / tamper tests carried over for v4.
- Benchmarks for the v4 path at the default size (Rule 34) to confirm no
  regression; `docs/PERFORMANCE.md` note.

## 5. Backward Compatibility

Default chunk size = current `ChunkSize = 4096`; existing callers need no change
to keep identical behavior. Enabling a non-default size is opt-in via
`SetStreamChunkSize`. Wire format bumps to v4 (breaking for any hypothetical
future reader that rejects unknown versions, but the project's own decoder
accepts v2/v3/v4). Complies with Rules 4/32 (bounded buffers), 5 (tests),
7 (documented versioned wire change), 33 (applies across all transports since
streaming is transport-agnostic), 38 (legacy versions preserved).
