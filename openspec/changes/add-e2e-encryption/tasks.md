## Phase 1: Transport TLS + Challenge-Response Auth

- [x] 1.1 Add config fields: `tls_cert`, `tls_key`, `tls_insecure` to `ConfigurationGlobal` struct.
- [x] 1.2 Parse new config fields in `loadGlobalConfig()`.
- [x] 1.3 Add TLS listener support to `src/transport/factory.go` — wrap `net.Listener` in `tls.Listener` when `tls_cert`/`tls_key` are set.
- [x] 1.4 Add TLS dial support to `src/transport/factory.go` — wrap `net.Conn` in `tls.Conn` for TCP protocols.
- [x] 1.5 Fix QUIC `InsecureSkipVerify` default to `false` — require `ca_cert` or explicit `tls_insecure = true`.
- [x] 1.6 Implement `ChallengeResponseServer(conn, authToken)` — generate 32-byte nonce, send, read HMAC response, verify with `hmac.Equal`.
- [x] 1.7 Implement `ChallengeResponseClient(conn, authToken)` — read nonce, compute `HMAC-SHA256(authToken, nonce)`, send response.
- [x] 1.8 Integrate challenge-response into `HandshakeServer` / `HandshakeClient` for momo-tcp and momo-quic.
- [x] 1.9 Write unit tests: TLS handshake, challenge-response success/failure, nonce replay protection, backward compatibility (no TLS = plaintext).
- [x] 1.10 Update `docs/PROTOCOL.md` with TLS and challenge-response handshake documentation.
- [x] 1.11 Run benchmarks to measure TLS handshake overhead.

## Phase 2: Crypto Package — AES-GCM-256, Convergent, Streaming

- [x] 2.1 Create `src/crypto/` package with `go.mod`.
- [x] 2.2 Implement `Encrypt(plaintext, key []byte) ([]byte, error)` — AES-GCM-256, 12-byte random IV, IV prepended to output.
- [x] 2.3 Implement `Decrypt(ciphertext, key []byte) ([]byte, error)` — extract IV, AES-GCM authenticated decryption.
- [x] 2.4 Implement `EncryptStream(reader io.Reader, key []byte) (io.Reader, error)` — 4KB chunked streaming AEAD.
- [x] 2.5 Implement `DecryptStream(reader io.Reader, key []byte) (io.Reader, error)` — streaming decryption with per-chunk auth tag verification.
- [x] 2.6 Implement `ConvergentEncrypt(plaintext []byte) (ciphertext, dedupKey []byte, err error)` — content-derived key via SHA-256, then AES-GCM-256.
- [x] 2.7 Implement `DeriveKey(masterKey []byte, tenantID string) ([]byte, error)` — HKDF-SHA256.
- [x] 2.8 Add config fields: `encryption_enabled`, `encryption_key`, `encryption_tenant` to `ConfigurationGlobal`.
- [x] 2.9 Parse and validate new config fields (64-char hex key, non-empty when enabled).
- [x] 2.10 Write unit tests: encrypt/decrypt round-trip, streaming round-trip, convergent dedup (identical plaintext → identical ciphertext), tamper detection, key derivation determinism, key derivation tenant isolation.
- [x] 2.11 Write benchmarks: encrypt/decrypt throughput, streaming overhead, convergent encryption overhead.

## Phase 3: E2EE momo Protocol — Content + Metadata Encryption

- [x] 3.1 Add encryption context to client: `src/client/client.go` — load tenant key from config when `encryption_enabled`.
- [x] 3.2 Encrypt file content before `io.Copy` in `Connect` / `ConnectStream` — use `EncryptStream` for large files.
- [x] 3.3 Decrypt file content after `io.Copy` on download — use `DecryptStream`.
- [x] 3.4 Encrypt filename + virtual path before sending to server — `Encrypt(wireName, tenantKey)`.
- [ ] 3.5 Decrypt filename on LIST response — decrypt each returned name. (Limitation: HMAC is one-way; client matches known files by recomputing HMAC.)
- [x] 3.6 Use convergent hash for CAS dedup key instead of plaintext hash — `ConvergentEncrypt` returns dedup key.
- [x] 3.7 Server-side: no changes to storage logic (already content-addressable). Server stores encrypted name → convergent hash, convergent hash → encrypted content.
- [x] 3.8 Ensure handshake metadata fields (filename, hash) are encrypted in `momo_tcp.go` and `momo_quic.go`.
- [x] 3.9 Write integration tests: E2EE upload/download round-trip, E2EE LIST with decrypted names, E2EE dedup (same content, different names → one blob), server zero-knowledge verification.
- [x] 3.10 Update `docs/PROTOCOL.md` with E2EE metadata encryption documentation.

## Phase 4: SSE S3 Fallback — Server-Side Encryption at Rest

- [x] 4.1 Create `EncryptedBlobStore` decorator in `src/storage/` implementing `BlobStore` interface.
- [x] 4.2 `PutBlob`: encrypt data with server-side derived key before writing to underlying store.
- [x] 4.3 `GetBlob`: decrypt data after reading from underlying store.
- [x] 4.4 `DeleteBlob`: passthrough to underlying store.
- [x] 4.5 Wire `EncryptedBlobStore` into server initialization when `encryption_enabled` and protocol is `s3-tcp` or `s3-quic`.
- [x] 4.6 S3 metadata (filenames) remain plaintext — no changes to S3 key handling.
- [x] 4.7 Dedup works on plaintext hash (server computes hash before encryption) — no convergent encryption for S3.
- [x] 4.8 Write integration tests: SSE PUT/GET round-trip, SSE with S3 client (aws-cli compatible), SSE backward compatibility (disabled = plaintext).
- [x] 4.9 Update `docs/PROTOCOL.md` with SSE fallback documentation.

## Phase 5: Per-Tenant Key Derivation

- [ ] 5.1 Integrate `DeriveKey(masterKey, tenantID)` into client encryption context — derive tenant key at startup.
- [ ] 5.2 Integrate `DeriveKey` into server SSE context — derive server-side tenant key for `EncryptedBlobStore`.
- [ ] 5.3 Tenant ID from `encryption_tenant` config (default: "default").
- [ ] 5.4 Write tests: tenant isolation (tenant A cannot decrypt tenant B's data), deterministic derivation, multi-tenant E2EE round-trip.
- [ ] 5.5 Update `docs/ARCHITECTURE.md` with per-tenant key derivation documentation.

## Cross-Cutting

- [ ] 6.1 Update `docs/PENTESTING.md` — mark CVE-009 as fixed by E2EE Phase 1.
- [ ] 6.2 Update `docs/ROADMAP.md` — add E2EE completion milestone.
- [ ] 6.3 Update `docs/ARCHITECTURE.md` — document encryption layers and protocol matrix.
- [ ] 6.4 Update `conf/momo.conf` example with new config fields (commented out).
- [ ] 6.5 Run full benchmark suite and document performance overhead in `docs/PERFORMANCE.md`.
- [ ] 6.6 Verify all 4 protocols work with and without encryption (protocol feature parity, Rule 33).
- [ ] 6.7 Verify backward compatibility — all existing tests pass with `encryption_enabled = false`.
