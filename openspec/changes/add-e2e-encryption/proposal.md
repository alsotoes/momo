# Proposal: Implement End-to-End Encryption (E2EE) — Most Complete Encryption

**Related Issue:** https://github.com/alsotoes/momo/issues/152
**Resolves CVE-009:** https://github.com/alsotoes/momo/issues/546

- **Champion:** opencode (glm-5.2)
- **Status:** `Draft`

## 1. Problem

All network traffic in `momo`, including file metadata, content, and authentication
tokens, is transmitted in plaintext over TCP connections. This is CVE-009 — a
critical security vulnerability that exposes data to eavesdropping,
man-in-the-middle (MitM) attacks, and token replay attacks. The system is
unsuitable for any environment that is not physically secured.

Additionally, storage nodes have full visibility into file contents, filenames,
and content hashes — there is zero server-side confidentiality.

## 2. Proposed Solution — 5-Phase Plan

Implement the **most complete encryption possible** across all 4 wire protocols
(`momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`), closing 4 design gaps:

| Gap | Description | Phase |
|-----|-------------|-------|
| 1 | Metadata (filenames) visible to server | Phase 3 |
| 2 | Dedup breaks under E2EE (different keys → different ciphertext) | Phase 2 |
| 3 | Auth token transmitted in plaintext | Phase 1 |
| 4 | No per-tenant key isolation | Phase 5 |

### Protocol Encryption Matrix

| Layer | momo-tcp | momo-quic | s3-tcp | s3-quic |
|-------|----------|-----------|--------|---------|
| Transport TLS | TLS 1.2/1.3 | TLS 1.3 (built-in) | TLS 1.2/1.3 | TLS 1.3 (built-in) |
| Auth | Challenge-response (HMAC) | Challenge-response (HMAC) | SigV4 / Bearer via TLS | SigV4 / Bearer via TLS |
| Content | **Client-side E2EE** | **Client-side E2EE** | SSE at rest | SSE at rest |
| Metadata | **Encrypted** | **Encrypted** | Plaintext (aws-cli) | Plaintext (aws-cli) |
| Dedup | Convergent | Convergent | Plaintext hash | Plaintext hash |
| Per-tenant | Yes | Yes | Yes | Yes |

**momo-tcp / momo-quic** achieve the **theoretical maximum**: true zero-knowledge
E2EE where the server never sees plaintext content, filenames, or the auth token.
This matches Storj and Tahoe-LAFS-level security.

**s3-tcp / s3-quic** achieve the **maximum possible for S3-compatible systems**:
transport encryption + server-side encryption at rest. The server sees plaintext
transiently during PUT/GET — this is a mathematical impossibility to avoid since
aws-cli sends plaintext HTTP. No S3-compatible system in existence does better
(AWS S3, MinIO, Storj gateway all have this same limitation).

## 3. Phase Breakdown

### Phase 1: Transport TLS + Challenge-Response Auth

**Goal:** Encrypt all traffic in transit and eliminate plaintext auth token transmission.

- Add TLS support to TCP-based protocols (`momo-tcp`, `s3-tcp`) via `crypto/tls`.
- Fix QUIC `InsecureSkipVerify` default — require CA cert or explicit opt-in.
- Implement HMAC-SHA256 challenge-response authentication:
  - Server sends 32-byte random nonce.
  - Client computes `HMAC-SHA256(auth_token, nonce)` and sends the response.
  - Server verifies the HMAC. The auth token is **never transmitted**.
- Config additions: `tls_cert`, `tls_key`, `tls_insecure` (default: false).
- **Resolves CVE-009** (#546).

### Phase 2: Crypto Package — AES-GCM-256, Convergent Encryption, Streaming

**Goal:** Create the cryptographic foundation with convergent encryption for dedup.

- New `src/crypto/` package:
  - `Encrypt(plaintext, key)` / `Decrypt(ciphertext, key)` — AES-GCM-256.
  - `EncryptStream(reader, key)` / `DecryptStream(reader, key)` — streaming AEAD
    via chunked AES-GCM (4KB chunks + per-chunk nonce + auth tag).
  - `ConvergentEncrypt(plaintext)` — content-derived key:
    1. `key = SHA-256(plaintext)` (content hash → key).
    2. `ciphertext = AES-GCM-256(plaintext, key)`.
    3. Dedup key = `SHA-256(ciphertext)` (convergent hash).
    - Identical plaintext → identical ciphertext → identical dedup key.
    - Server learns content equality but NOT content itself.
  - `DeriveKey(masterKey, tenantID)` — HKDF-SHA256 per-tenant key derivation.
- Config additions: `encryption_enabled` (default: false), `encryption_key`
  (64-char hex = 256-bit master key), `encryption_tenant` (default: "default").
- Unit tests: encrypt/decrypt round-trip, streaming, convergent dedup, tamper
  detection, key derivation.

### Phase 3: E2EE momo Protocol — Content + Metadata Encryption

**Goal:** True zero-knowledge E2EE for `momo-tcp` and `momo-quic`.

- **Content encryption:** Client encrypts file payload before `io.Copy` to
  transport. Server stores only ciphertext. Client decrypts on download.
- **Metadata encryption:** Client encrypts the filename (and virtual path)
  before sending to server. Server stores `Encrypt(filename, tenantKey)` as the
  storage key. Server cannot read filenames — only the client with the key can.
- **Hash adaptation:** The content hash used for CAS dedup is the *convergent*
  hash (from Phase 2), not the plaintext hash. This preserves dedup under E2EE.
- Integration points:
  - `src/client/client.go`: `Connect` / `ConnectStream` — encrypt before send,
    decrypt after receive.
  - `src/server/server.go`: Store encrypted name + encrypted content. No changes
    to storage logic (it's already content-addressable).
  - `src/transport/momo_tcp.go` / `momo_quic.go`: Handshake sends encrypted
    metadata fields.

### Phase 4: SSE S3 Fallback — Server-Side Encryption at Rest

**Goal:** Maximum encryption for S3-compatible protocols.

- Create `EncryptedBlobStore` decorator wrapping the `BlobStore` interface:
  - `PutBlob`: encrypts data with server-side key before writing to underlying
    store.
  - `GetBlob`: decrypts data after reading from underlying store.
  - `DeleteBlob`: passthrough.
- Server-side key from `encryption_key` config (same master key, derived per
  tenant via HKDF).
- The server sees plaintext transiently during PUT/GET (unavoidable for S3).
- S3 metadata (filenames) remain plaintext — aws-cli requires readable keys.
- Dedup works on plaintext hash (server computes hash before encryption).

### Phase 5: Per-Tenant Key Derivation

**Goal:** Cryptographic isolation between tenants.

- `DeriveKey(masterKey, tenantID)` uses HKDF-SHA256 to derive a unique 256-bit
  key per tenant.
- Each tenant's data is encrypted with their derived key.
- Tenant A cannot decrypt Tenant B's data even if they share the same master key.
- Tenant ID from `encryption_tenant` config (default: "default").
- Future: per-request tenant ID from auth context (requires multi-tenant auth
  extension, out of scope for this proposal).

## 4. Performance Analysis & Justification

- **Expected Performance Impact:**
  1. **CPU:** AES-GCM-256 encryption/decryption adds CPU overhead. Modern CPUs
     with AES-NI instructions minimize this (~10-15% throughput reduction for
     bulk transfers).
  2. **Latency:** Per-chunk auth tag verification adds ~microseconds per 4KB
     chunk. Negligible for typical file sizes.
  3. **Convergent encryption:** Adds one extra SHA-256 pass over the content.
     Minimal overhead relative to the AES-GCM encryption itself.
  4. **TLS handshake:** One-time ~1-2ms per connection. Amortized over the
     connection lifetime.

- **Justification:** The performance cost is **absolutely necessary**. Plaintext
  transmission is not viable for any production system. The cost of a data breach
  far outweighs the computational cost of encryption.

- **Measurement Plan:**
  1. **Baseline:** `make benchmark COUNT=10` on current `master`.
  2. **Post-Implementation:** Same benchmarks with E2EE enabled.
  3. **Analysis:** Measure `ns/op` increase, CPU usage, and throughput. Document
     the overhead precisely. The change is successful if security goals are met
     and overhead is within an acceptable, documented range.

## 5. Security Properties Achieved

| Property | momo protocols | s3 protocols |
|----------|---------------|--------------|
| Confidentiality (content) | ✅ Client-side E2EE | ✅ SSE at rest + TLS in transit |
| Confidentiality (metadata) | ✅ Encrypted filenames | ❌ Plaintext (aws-cli constraint) |
| Integrity | ✅ AES-GCM auth tag | ✅ AES-GCM auth tag |
| Auth token protection | ✅ Challenge-response (HMAC) | ✅ TLS + SigV4 |
| Transport encryption | ✅ TLS 1.2/1.3 | ✅ TLS 1.2/1.3 |
| Dedup under encryption | ✅ Convergent encryption | ✅ Plaintext hash |
| Per-tenant isolation | ✅ HKDF key derivation | ✅ HKDF key derivation |
| Server zero-knowledge | ✅ Server sees nothing | ❌ Server sees plaintext transiently |

## 6. Backward Compatibility

- E2EE is **opt-in** via `encryption_enabled = false` (default).
- When disabled, all protocols behave exactly as before (plaintext).
- TLS is **opt-in** via `tls_cert` / `tls_key` (empty = plaintext TCP).
- QUIC `InsecureSkipVerify` defaults to `false` (breaking change for deployments
  without CA certs — they must set `tls_insecure = true` explicitly).

---
