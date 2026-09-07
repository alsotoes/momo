# 0048-secure-e2ee-confidential-dedup

## Status
Accepted

## Confidence
High

## Context
The previously-merged E2EE implementation (`encryption_enabled`, AES-GCM-256
content encryption, deterministic stream encryption, convergent dedup) contains
serious cryptographic flaws that materially weaken confidentiality and, in one
case, enable complete keystream compromise:

1. **CRITICAL — GCM nonce reuse in `EncryptStream`** (`src/crypto/streaming.go`):
   the per-chunk nonce is a 4-byte counter reset to `0` on every stream. Because
   content is encrypted with the **same master key** across all files, the first
   chunk of every file uses nonce `0x00000000…`, the second `0x00000001…`, etc.
   AES-GCM reuse of a (key, nonce) pair for two different plaintexts leaks the
   XOR of the two plaintexts and, with a known-plaintext oracle, allows
   construction of forged ciphertext — **catastrophic**. This is the same class
   of bug as the 2018 CVE-2018-0495 OpenSSH nonce-reuse flaw.
2. **CONFIDENTIALITY — Deterministic encryption enables offline attacks**: the
   fixed (key, nonce) per file means identical plaintext encrypts to identical
   ciphertext, so the server (which stores ciphertext) can perform offline
   brute-force and equality checks against known/suspect plaintext without any
   key material. The client's `Hash = SHA-256(ciphertext)` is deterministic for
   the same data, defeating the purpose of per-tenant keys.
3. **Tenant key unused for content**: `DeriveKey` (HKDF-SHA256) is defined and
   validated but content payloads are encrypted with the **raw master key**
   (`client.go:50` uses `NewCipherFromHex(cfg.Global.EncryptionKey)`; the
   derived tenant key is used only for filename HMAC). `PROTOCOL.md` claims
   per-tenant key isolation for data content; this is **false**.
4. **Full-file RAM buffering**: `client.go` buffers the entire encrypted file in
   memory (`var encBuf bytes.Buffer`, `encBuf.Bytes()`) before sending. For a
   1 GiB file (`MaxFileSize`), peak heap grows ~1 GiB — a DoS vector and a
   scalability limit.
5. **HKDF domain-separation collision**: `DeriveKey(master, tenant, context)`
   concatenates `tenant + context` with no length delimiter or domain labels.
   e.g. `tenant="ab", context="c"` yields `info="abc"` identical to
   `tenant="a", context="bc"` — two different logical keys collapse to the same
   derived key.
6. **At-rest hygiene**: `EncryptedBlobStore` re-encrypts with the same master
   key and the same buggy deterministic stream; the CAS hash is the plaintext
   hash (good) but the encryption is the same broken scheme.

Additionally, dedup and confidentiality conflict: to dedup identical plaintext
across clients the encryption key must be content-derived (deterministic), but a
deterministic content-derived key lets the server brute-force it. The old
"convergent encryption" (`convergent.go`) used `key = SHA-256(plaintext)`, which
is exactly this offline-attack weakness.

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
- **Blog post**: docs/blog/posts/014-confidential-dedup-oprf.md

## References
- Issue: #688
- PR: #819
- Spec: `openspec/changes/secure-e2ee-confidential-dedup/`
- Blog: docs/blog/posts/014-confidential-dedup-oprf.md

