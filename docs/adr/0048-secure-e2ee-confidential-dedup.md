# 0048-secure-e2ee-confidential-dedup

## Status
Accepted

## Confidence
High

## Context


## Decision
- Streaming AEAD Nonce Non-Reuse (Phase A): The streaming AEAD (`EncryptStream`/`DecryptStream` in `src/crypto/streaming.go`) SHALL generate a per-stream random seed such that the (key, nonce) pair is never reused across streams for the same key. The nonce SHALL be formed as `nonce[0:8] = randomSeed`, `nonce[8:12] = big-endian chunkIndex`. This removes the prior per-stream 4-byte counter that reset to zero on every stream.
- Domain-Separated Key Derivation (Phase A): `DeriveKey` in `src/crypto/crypto.go` SHALL construct HKDF `info` from length-encoded, domain-labeled parts so that no two distinct (label, tenant, context) tuples collide.
- Content Encryption Uses the Tenant-Derived Key (Phase A): The client SHALL encrypt content payloads with the tenant-derived content key `DeriveKey(masterKey, tenant, "momo/content")`, NOT the raw master key. The server's SSE `EncryptedBlobStore` SHALL use `DeriveKey(masterKey, tenant, "momo/atrest")`.
- Threshold OPRF for Confidential Dedup (Phase B): The system SHALL derive the content key from the plaintext dedup tag via a threshold OPRF evaluated over a quorum of daemons. The OPRF secret SHALL be split so that no single server holds it. The CAS/dedup key SHALL remain `H(plaintext)`. The operation SHALL fail closed (abort, no convergent fallback) when fewer than `threshold` OPRF evaluations are available.
- Streaming Client (memory-bound upload) (Phase A): The client upload path SHALL encrypt content via a streaming pipe rather than buffering the full file in memory. Peak heap for upload SHALL be proportional to chunk size, not file size.
- Removal of Deterministic Convergent Encryption (Phase A): `src/crypto/convergent.go` (deterministic encryption keyed by `SHA-256(plaintext)` with a zero nonce) SHALL be removed and replaced by the threshold OPRF at the content layer. No code path SHALL fall back to convergent encryption.
- Configuration (Phase B): The configuration SHALL support the following new/changed fields in the `[global]` section: | Field | Type | Default | Description | |-------|------|---------|-------------| | `oprf_enabled` | bool | = `encryption_enabled` | Enable threshold-OPRF confidential dedup | | `oprf_threshold` | int | derived from cluster size | Minimum daemon evaluations required (fail-closed) | `oprf_threshold` SHALL be validated to be at least `1` and at most the number of configured daemons.
- Backward Compatibility (Phase A & B): When `encryption_enabled = false`, all protocols SHALL behave exactly as before (plaintext), and no existing plaintext deployment breaks on upgrade. ---

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
- Spec: openspec/changes/secure-e2ee-confidential-dedup/
- Blog: docs/blog/posts/...md
