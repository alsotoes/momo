> GitHub Issue URL: https://github.com/alsotoes/momo/issues/688

# Secure E2EE — Confidential Dedup via Threshold OPRF + Crypto Hardening

## Purpose

This specification hardens the existing end-to-end encryption (E2EE) layer and
introduces a threshold Oblivious PRF (OPRF) so the cluster can **deduplicate
identical plaintext while keeping content keys computationally hidden from every
server**. It eliminates critical nonce-reuse, fixes tenant-key isolation and
HKDF domain separation, removes deterministic convergent encryption, streams
content instead of buffering whole files, and fails closed when the OPRF quorum
is not met.

## ADDED Requirements

### Requirement: Streaming AEAD Nonce Non-Reuse (Phase A)

The streaming AEAD (`EncryptStream`/`DecryptStream` in `src/crypto/streaming.go`)
SHALL generate a per-stream random seed such that the (key, nonce) pair is
never reused across streams for the same key. The nonce SHALL be formed as
`nonce[0:8] = randomSeed`, `nonce[8:12] = big-endian chunkIndex`. This removes
the prior per-stream 4-byte counter that reset to zero on every stream.

#### Scenario: two streams encrypted with the same key do not reuse a nonce
- **GIVEN** a single encryption key `K` and two distinct plaintexts `P1, P2`
- **WHEN** `EncryptStream(P1, K)` and `EncryptStream(P2, K)` are executed
- **THEN** the first chunk of each stream carries a different 8-byte random
  seed, so chunk `i` of stream 1 and chunk `i` of stream 2 are sealed under
  different nonces, and the keystreams differ.

#### Scenario: legacy stream version is rejected
- **GIVEN** a stream whose header byte is `StreamVersion == 1` (the insecure
  scheme)
- **WHEN** `DecryptStream` reads its header
- **THEN** it returns an explicit unsupported-version error and outputs no
  plaintext.

### Requirement: Domain-Separated Key Derivation (Phase A)

`DeriveKey` in `src/crypto/crypto.go` SHALL construct HKDF `info` from
length-encoded, domain-labeled parts so that no two distinct
(label, tenant, context) tuples collide.

#### Scenario: colliding concatenations no longer produce the same key
- **GIVEN** the previously-colliding inputs `DeriveKey(K, "ab", "c")` and
  `DeriveKey(K, "a", "bc")`
- **WHEN** both are derived under the new scheme
- **THEN** the resulting keys are different (integration test asserts
  inequality).

#### Scenario: distinct domain labels produce distinct keys
- **GIVEN** a master key `K` and tenant `T`
- **WHEN** `DeriveKey(K, T, "momo/content")` and `DeriveKey(K, T, "momo/atrest")`
  are computed
- **THEN** the two keys differ.

### Requirement: Content Encryption Uses the Tenant-Derived Key (Phase A)

The client SHALL encrypt content payloads with the tenant-derived content key
`DeriveKey(masterKey, tenant, "momo/content")`, NOT the raw master key. The
server's SSE `EncryptedBlobStore` SHALL use `DeriveKey(masterKey, tenant,
"momo/atrest")`.

#### Scenario: tenant A cannot read tenant B content
- **GIVEN** two tenants with a shared master key but different tenant IDs
- **WHEN** each tenant encrypts identical plaintext with its derived content key
- **THEN** neither tenant's key decrypts the other's ciphertext.

### Requirement: Threshold OPRF for Confidential Dedup (Phase B)

The system SHALL derive the content key from the plaintext dedup tag via a
threshold OPRF evaluated over a quorum of daemons. The OPRF secret SHALL be
split so that no single server holds it. The CAS/dedup key SHALL remain
`H(plaintext)`. The operation SHALL fail closed (abort, no convergent
fallback) when fewer than `threshold` OPRF evaluations are available.

#### Scenario: same plaintext yields the same content key across clients
- **GIVEN** two clients (possibly different tenants) with the same plaintext `P`
- **WHEN** both compute `tag = H(P)` and run the threshold OPRF over a
  satisfied quorum
- **THEN** both derive the same content key, and the server dedups both uploads
  onto a single blob under `tag`.

#### Scenario: server cannot derive the content key offline
- **GIVEN** a server stores ciphertext and dedup tag `H(P)`
- **WHEN** it attempts to compute the content key using only its own state
- **THEN** it cannot, because the OPRF secret is split across at least
  `threshold` daemons and no single daemon can evaluate the OPRF on `H(P)`
  alone.

#### Scenario: fail-closed when the quorum is not met
- **GIVEN** a configured quorum `threshold = t` and fewer than `t` daemons
  respond to an OPRF evaluation request
- **WHEN** the client attempts to derive a content key
- **THEN** the operation aborts with an explicit error and the upload is NOT
  persisted under a weaker (convergent) scheme.

#### Scenario: OPRF evaluation via P2P RPC
- **GIVEN** a daemon configured with an OPRF key share
- **WHEN** it receives an OPRF evaluation RPC over the P2P transport containing
  a blinded dedup tag
- **THEN** it returns the share evaluation and never receives or can recover the
  unblinded tag or the resulting content key.

### Requirement: Streaming Client (memory-bound upload) (Phase A)

The client upload path SHALL encrypt content via a streaming pipe rather than
buffering the full file in memory. Peak heap for upload SHALL be proportional to
chunk size, not file size.

#### Scenario: uploading a large file keeps memory bounded
- **GIVEN** a file near `MaxFileSize`
- **WHEN** it is uploaded with encryption enabled
- **THEN** the client does not allocate a buffer proportional to the file size;
  it streams encrypted chunks.

### Requirement: Removal of Deterministic Convergent Encryption (Phase A)

`src/crypto/convergent.go` (deterministic encryption keyed by `SHA-256(plaintext)`
with a zero nonce) SHALL be removed and replaced by the threshold OPRF at the
content layer. No code path SHALL fall back to convergent encryption.

#### Scenario: convergent API is gone
- **GIVEN** the source tree after implementation
- **WHEN** searching for `ConvergentEncrypt` / `convergent.go`
- **THEN** no reachable implementation remains (build + tests pass without it).

### Requirement: Configuration (Phase B)

The configuration SHALL support the following new/changed fields in the
`[global]` section:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `oprf_enabled` | bool | = `encryption_enabled` | Enable threshold-OPRF confidential dedup |
| `oprf_threshold` | int | derived from cluster size | Minimum daemon evaluations required (fail-closed) |

`oprf_threshold` SHALL be validated to be at least `1` and at most the number of
configured daemons.

#### Scenario: invalid threshold rejected
- **GIVEN** `oprf_threshold = 0` or `oprf_threshold > len(daemons)`
- **WHEN** the server starts
- **THEN** startup fails with an explicit validation error.

### Requirement: Backward Compatibility (Phase A & B)

When `encryption_enabled = false`, all protocols SHALL behave exactly as
before (plaintext), and no existing plaintext deployment breaks on upgrade.

---