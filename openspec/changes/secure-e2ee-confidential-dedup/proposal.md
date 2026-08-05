# Proposal: Secure E2EE — Confidential Dedup via Threshold OPRF + Crypto Hardening

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/688

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`
- **Supersedes / hardens:** `openspec/changes/add-e2e-encryption/` (merged) which
  contained critical cryptographic flaws.

## 1. Problem

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

## 2. Proposed Solution — 3-Phase Plan

Keep **both** dedup and confidentiality using a **threshold OPRF**
(server-aided Message-Locked Encryption, DupLESS / MLE construction):

- **Dedup tag** (what the server stores and dedups on) remains the hash of the
  plaintext: `H(plaintext)`. This preserves content-addressable dedup across
  clients.
- **Confidential content key** is the OPRF output evaluated over a **threshold
  quorum** of daemons: `K = OPRF_{sk}(H(plaintext))`. The OPRF secret key `sk`
  is split across the cluster on the P2P transport; **no single server** holds
  `sk` or can compute `OPRF_{sk}(x)` from `x` alone. The server therefore cannot
  derive content keys by offline brute-force, even though it stores ciphertext
  and the dedup tag.
- **Fail-closed**: if fewer than `threshold` OPRF evaluations return, the
  operation **aborts** — there is **no** convergent-encryption fallback.

The OPRF provides a *deterministic* key per distinct dedup tag (so the same
plaintext across clients still maps to the same content key → dedup preserved)
while remaining **computationally hidden** from the server (confidentiality).

### 2.1 Why threshold OPRF (not convergent, not single-server OPRF)

| Scheme | Dedup | Confidentiality vs server | Server can offline-brute-force |
|--------|-------|---------------------------|---------------------------------|
| Convergent (`key=SHA256(P)`) | ✅ | ❌ `key` derivable from `P` | ✅ — knows `H(P)`, or guesses `P` |
| Single-server OPRF | ✅ | ⚠️ server owns `sk` | ✅ — server holds `sk`, can eval any `x` |
| **Threshold OPRF (this)** | ✅ | ✅ no party holds `sk` | ❌ — needs `t` servers to evaluate |

### 2.2 Protocol Encryption Matrix (unchanged from prior E2EE spec)

| Layer | momo-tcp | momo-quic | s3-tcp | s3-quic |
|-------|----------|-----------|--------|---------|
| Transport TLS | TLS 1.2/1.3 | TLS 1.3 | TLS 1.2/1.3 | TLS 1.3 |
| Auth | Challenge-response (HMAC) | Challenge-response (HMAC) | SigV4 / Bearer via TLS | SigV4 / Bearer via TLS |
| Content | Client-side E2EE | Client-side E2EE | SSE at rest | SSE at rest |
| Metadata | Encrypted | Encrypted | Plaintext (aws-cli) | Plaintext (aws-cli) |
| Dedup | **Threshold OPRF** | **Threshold OPRF** | Plaintext hash | Plaintext hash |
| Per-tenant | Yes | Yes | Yes | Yes |

## 3. Phase Breakdown

### Phase A: Crypto Hardening (nonce, key derivation, at-rest, streaming)

**Goal:** Eliminate the critical flaws in the existing crypto.

1. **Nonce fix (`streaming.go`)**: derive each chunk's nonce from a **random
   per-stream 8-byte seed XORed into the counter**, so a (key, nonce) pair is
   never reused across streams. `nonce[0:8] = randomSeed`, `nonce[8:12] =
   chunkIndex`. Bump `StreamVersion = 2`.
2. **Domain-separated HKDF (`crypto.go`)**: rewrite `DeriveKey` to use a
   structured `info` with **length-encoded parts + explicit domain labels**:
   `info = label(len) || label || tenant(len) || tenant || context(len) ||
   context`, so no two (label, tenant, context) triples collide. Add domain
   labels: `"momo/token"`, `"momo/content"`, `"momo/atrest"`, `"momo/oprf"`.
3. **Content key = tenant-derived**: client must encrypt content with
   `DeriveKey(masterKey, tenant, "momo/content")`, not the raw master key.
   Server SSE must use `DeriveKey(masterKey, tenant, "momo/atrest")`.
4. **Streaming client**: replace the full-file `bytes.Buffer` with a streaming
   pipe so peak memory is chunk-sized (mirrors `EncryptedBlobStore`).
5. **At-rest hygiene**: `EncryptedBlobStore` uses
   `DeriveKey(masterKey, tenant, "momo/atrest")` and the fixed streaming scheme.
6. **Remove `convergent.go`** (and its zero-nonce deterministic SSE) — replaced
   by threshold OPRF at the content layer (Phase B).

### Phase B: Threshold OPRF + P2P OPRF RPC

**Goal:** confidential dedup via a threshold OPRF across the cluster.

1. Add `github.com/bytemare/crypto` dependency (group primitives: ristretto255,
   `HashToGroup`, `Element.Multiply/Add`, `Scalar.Invert`).
2. New `src/crypto/oprf.go`:
   - `OPRFBlind(input) (blinded, blind, err)` — blind the dedup tag.
   - `OPRFEvaluateShare(shareScalar, blinded) → evaluation` (per-server share).
   - `OPRFCombineUnblind(evaluations[], blind) → contentKey` — combine `t`
     evaluations and unblind; **error if `len < t`** (fail-closed).
3. **P2P OPRF RPC**: add P2P message types (see tasks): client dispatches
   blinded evaluations to a set of daemons; each daemon returns its OPRF share
   evaluation. Mirrors the `ScatterGather.HandleRPC` routing in
   `src/p2p/gossip.go`.
4. **Client integration**: on upload, compute `tag = H(plaintext)`, blind it,
   collect `t` OPRF share evaluations over the cluster, derive content key,
   encrypt content streaming with the tenant content key, and store `tag` as the
   CAS hash (dedup preserved). **No convergent fallback.**
5. Config: `oprf_threshold` (default derived from cluster size), `oprf_enabled`
   (default matches `encryption_enabled`).

### Phase C: Tests, Docs, Verification

1. Unit tests: nonce non-reuse, domain-separated derivation (collision tests),
   OPRF round-trip + threshold fail-closed, content key = tenant-derived.
2. Integration: E2EE upload/download round-trip, E2EE dedup (same content,
   different names → one blob), tenant isolation, fail-closed (fewer than `t`
   daemons → abort).
3. Docs: rewrite `docs/PROTOCOL.md` E2EE + OPRF sections; update
   `docs/CONFIGURATION.md`, `docs/ARCHITECTURE.md`; `README.md` parity (Rule 27).

## 4. Performance Analysis & Justification

- **Nonce fix:** negligible — adds 8 random bytes of RNG per stream.
- **Domain-separated HKDF:** negligible — one HKDF call per operation.
- **Streaming client:** *reduces* peak memory from O(file) to O(chunk) — a
  strict improvement.
- **OPRF:** one hash-to-curve on the tag + `t` scalar multiplications. Cost is
  O(t) group ops (~`t`·~µs) per upload, amortized over the file size. The OPRF
  is on the 32-byte dedup tag, not the payload, so it is constant-time
  regardless of file size.
- **Measurement plan:** `make benchmark COUNT=10` against `master`, then with
  E2EE + OPRF enabled; document overhead in `docs/PERFORMANCE.md`.

## 5. Security Properties Achieved

| Property | Value |
|----------|-------|
| Confidentiality (content) | ✅ client-side E2EE, threshold-OPRF derived key |
| Confidentiality (metadata/mom0) | ✅ encrypted filenames |
| Integrity | ✅ AES-GCM-256 auth tag |
| Nonce non-reuse | ✅ random per-stream seed |
| Offline brute-force resistance | ✅ key hidden from server via threshold OPRF |
| Dedup under encryption | ✅ content-addressable on `H(plaintext)` |
| Per-tenant isolation (content) | ✅ tenant-derived content key |
| At-rest on-momemo storage | ✅ domain-separated at-rest key |
| Server zero-knowledge | ✅ server cannot derive content keys (no party holds `sk`) |
| Fail-closed | ✅ abort when `#eva< threshold`, no convergent fallback |

## 6. Backward Compatibility

- E2EE + OPRF remain **opt-in** (`encryption_enabled`, default `false`).
- With encryption disabled, all behavior is unchanged (plaintext).
- **Breaking within E2EE:** existing E2EE-encrypted blobs written with
  `StreamVersion=1` and the raw-master-key/zero-nonce scheme are **not
  decryptable** by the new scheme. Since E2EE is opt-in and this is a security
  rewrite of an unreleased flawed scheme, existing E2EE data is treated as
  non-recoverable; the new implementation warns on legacy stream version.
- Tenant key for content changes from "none (raw master)" to "derived" — a
  deliberate confidentiality fix, documented as a breaking change for the E2EE
  feature only.

---