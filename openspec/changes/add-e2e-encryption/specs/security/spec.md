> GitHub Issue URL: https://github.com/alsotoes/momo/issues/152
> CVE-009 Issue URL: https://github.com/alsotoes/momo/issues/546

# End-to-End Encryption (E2EE) Specification — Most Complete Encryption

## Purpose

This specification defines the cryptographic protocols, key management schemas,
and execution flows for the most complete encryption possible across all 4 wire
protocols (`momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`). It implements 5 phases:
transport TLS, challenge-response auth, client-side E2EE with metadata encryption,
convergent encryption for dedup, SSE fallback for S3, and per-tenant key isolation.

## ADDED Requirements

### Requirement: Transport TLS Encryption (Phase 1, Resolves CVE-009 #546)

All TCP-based wire protocols (`momo-tcp`, `s3-tcp`) SHALL support TLS 1.2/1.3
when `tls_cert` and `tls_key` are configured. QUIC protocols (`momo-quic`,
`s3-quic`) already use TLS 1.3 via QUIC but SHALL default `InsecureSkipVerify`
to `false`, requiring either a CA certificate (`ca_cert`) or explicit opt-in
(`tls_insecure = true`).

#### Scenario: TCP connection with TLS enabled
- **GIVEN** the server is configured with `tls_cert` and `tls_key` paths
- **WHEN** a client connects using `momo-tcp` or `s3-tcp`
- **THEN** the server wraps the `net.Listener` in a `tls.Listener` and the
  client wraps the `net.Conn` in a `tls.Conn`, establishing an encrypted channel
  before any application data is exchanged

#### Scenario: TCP connection without TLS (backward compatible)
- **GIVEN** `tls_cert` and `tls_key` are not configured (empty)
- **WHEN** a client connects
- **THEN** the connection uses plaintext TCP, preserving backward compatibility

#### Scenario: QUIC without CA certificate
- **GIVEN** `ca_cert` is not configured and `tls_insecure` is not set to `true`
- **WHEN** a QUIC client dials a peer
- **THEN** the connection fails with an explicit error: "QUIC peer verification
  requires ca_cert or tls_insecure=true"

#### Scenario: QUIC with explicit insecure opt-in
- **GIVEN** `tls_insecure = true` is explicitly set
- **WHEN** a QUIC client dials a peer
- **THEN** the connection proceeds with `InsecureSkipVerify = true` and a warning
  is logged: "TLS verification disabled — connections are vulnerable to MitM"

### Requirement: Challenge-Response Authentication (Phase 1, Resolves CVE-009 #546)

The authentication token SHALL never be transmitted in plaintext. Instead, the
server SHALL send a 32-byte cryptographically secure random nonce, and the client
SHALL respond with `HMAC-SHA256(auth_token, nonce)`. The server verifies the HMAC
without ever receiving the token.

#### Scenario: Successful challenge-response handshake
- **GIVEN** a client holds auth token `T` and connects to a server
- **WHEN** the handshake begins
- **THEN** the server generates a 32-byte random nonce `N` and sends it to the
  client, the client computes `R = HMAC-SHA256(T, N)` and sends `R` to the server,
  the server computes `HMAC-SHA256(T, N)` using its stored token and compares it
  to `R` using `hmac.Equal` (constant-time comparison), and if they match, the
  connection is authenticated

#### Scenario: Failed challenge-response (wrong token)
- **GIVEN** a client holds an incorrect auth token
- **WHEN** the challenge-response handshake is performed
- **THEN** the HMAC comparison fails, the server immediately closes the connection,
  logs "authentication failed: HMAC mismatch", and returns `syscall.EACCES`

#### Scenario: Nonce replay protection
- **GIVEN** an attacker captures a nonce-response pair from a previous handshake
- **WHEN** the attacker attempts to replay the response for a new connection
- **THEN** the server generates a new random nonce for each connection, the
  replayed response does not match the new nonce's expected HMAC, and the
  connection is rejected

### Requirement: AES-GCM-256 Content Encryption (Phase 2)

The `src/crypto` package SHALL provide authenticated symmetric encryption using
AES-GCM-256 for both in-memory and streaming operations.

#### Scenario: Encrypting and decrypting a payload (round-trip)
- **GIVEN** a 256-bit symmetric key `K` and plaintext bytes `P`
- **WHEN** `C = Encrypt(P, K)` and `P' = Decrypt(C, K)`
- **THEN** `P' == P` (round-trip succeeds), `C` contains a 12-byte IV prefix
  followed by ciphertext and a 16-byte GCM auth tag, and `C` is different for
  each call (random IV)

#### Scenario: Streaming encryption of large files
- **GIVEN** a file larger than available memory
- **WHEN** the client encrypts the file using `EncryptStream(reader, key)`
- **THEN** the file is processed in 4KB chunks, each chunk is encrypted with a
  unique per-chunk nonce, and the output is a stream of `[nonce(12B) | ciphertext
  | auth_tag(16B)]` envelopes that can be decrypted in the same chunked order

#### Scenario: Decryption failure on tampered ciphertext
- **GIVEN** a ciphertext envelope `C` that has been modified by an attacker
- **WHEN** `Decrypt(C, K)` is executed
- **THEN** the GCM auth tag verification fails, the function returns an error
  wrapping `syscall.EBADMSG`, and no plaintext is output

### Requirement: Convergent Encryption for Dedup (Phase 2, Gap 2)

The system SHALL support convergent encryption where the encryption key is derived
from the content itself, enabling deduplication of identical plaintext across
different clients while maintaining confidentiality from the server.

#### Scenario: Identical plaintext produces identical ciphertext
- **GIVEN** two clients with different tenant keys but the same plaintext `P`
- **WHEN** both clients call `ConvergentEncrypt(P)`
- **THEN** both derive `key = SHA-256(P)`, both produce identical ciphertext
  `C = AES-GCM-256(P, key)`, and both compute the same dedup key
  `D = SHA-256(C)`, enabling server-side dedup without the server knowing `P`

#### Scenario: Different plaintext produces different ciphertext
- **GIVEN** two different plaintexts `P1 != P2`
- **WHEN** `ConvergentEncrypt(P1)` and `ConvergentEncrypt(P2)` are called
- **THEN** the resulting ciphertexts and dedup keys are different

### Requirement: Per-Tenant Key Derivation (Phase 5, Gap 4)

The system SHALL derive a unique 256-bit encryption key per tenant using
HKDF-SHA256 from a master key, ensuring cryptographic isolation between tenants.

#### Scenario: Key derivation for different tenants
- **GIVEN** a master key `M` and two tenant IDs `T1 != T2`
- **WHEN** `K1 = DeriveKey(M, T1)` and `K2 = DeriveKey(M, T2)`
- **THEN** `K1 != K2`, data encrypted with `K1` cannot be decrypted with `K2`,
  and both `K1` and `K2` are 256-bit keys

#### Scenario: Deterministic key derivation
- **GIVEN** the same master key `M` and tenant ID `T`
- **WHEN** `DeriveKey(M, T)` is called multiple times
- **THEN** all calls return the same key (deterministic, no randomness)

### Requirement: E2EE momo Protocol — Content + Metadata Encryption (Phase 3, Gaps 1 & 3)

For `momo-tcp` and `momo-quic`, the client SHALL encrypt both file content AND
metadata (filenames, virtual paths) before transmitting to the server. The server
stores only encrypted content and encrypted names, achieving zero-knowledge storage.

#### Scenario: Uploading a file with E2EE enabled
- **GIVEN** a client with E2EE enabled, tenant key `K`, and a file named
  `docs/report.pdf` with content `P`
- **WHEN** the client uploads the file
- **THEN** the client encrypts the content: `C = Encrypt(P, K)`, encrypts the
  filename: `encName = Encrypt("docs/report.pdf", K)`, computes the convergent
  dedup hash: `D = SHA-256(C)`, and sends `(encName, D, C)` to the server. The
  server stores `encName → D` and `D → C`. The server cannot read the filename
  or the content.

#### Scenario: Downloading a file with E2EE enabled
- **GIVEN** a client with tenant key `K` requests file `docs/report.pdf`
- **WHEN** the client initiates the download
- **THEN** the client encrypts the filename: `encName = Encrypt("docs/report.pdf", K)`,
  requests the server for `encName`, receives encrypted content `C`, and decrypts:
  `P = Decrypt(C, K)`, streaming the plaintext back to the user

#### Scenario: Listing files with E2EE enabled
- **GIVEN** a client with tenant key `K` requests a file listing
- **WHEN** the server returns the list of encrypted names
- **THEN** the client decrypts each name using `Decrypt(encName, K)` and presents
  the plaintext filenames to the user. The server cannot read any filename.

#### Scenario: Server has zero knowledge
- **GIVEN** E2EE is enabled for momo protocols
- **WHEN** an attacker gains full access to the server's storage
- **THEN** the attacker sees only: encrypted filenames (AES-GCM-256 ciphertext),
  encrypted content (AES-GCM-256 ciphertext), and convergent hashes (SHA-256 of
  ciphertext). The attacker cannot derive plaintext without the tenant key.

### Requirement: SSE S3 Fallback — Server-Side Encryption at Rest (Phase 4)

For `s3-tcp` and `s3-quic`, the server SHALL encrypt data at rest using an
`EncryptedBlobStore` decorator. The server sees plaintext transiently during
PUT/GET (unavoidable for S3-compatible protocols).

#### Scenario: S3 PUT with SSE enabled
- **GIVEN** a server with SSE enabled and a derived server-side key `SK`
- **WHEN** an S3 client uploads plaintext `P` for key `K`
- **THEN** the server receives `P`, computes `C = Encrypt(P, SK)`, stores `C` at
  the key `K`, and the stored data is encrypted. The server saw `P` transiently.

#### Scenario: S3 GET with SSE enabled
- **GIVEN** a server with SSE enabled and stored ciphertext `C` for key `K`
- **WHEN** an S3 client requests key `K`
- **THEN** the server reads `C` from storage, computes `P = Decrypt(C, SK)`, and
  returns `P` to the client. The server sees `P` transiently.

#### Scenario: S3 without SSE (backward compatible)
- **GIVEN** `encryption_enabled = false`
- **WHEN** an S3 client uploads or downloads data
- **THEN** the server stores and returns plaintext without any encryption

### Requirement: Configuration (All Phases)

The configuration SHALL support the following new fields in the `[global]` section:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `tls_cert` | string | "" | Path to PEM-encoded TLS certificate (TCP protocols) |
| `tls_key` | string | "" | Path to PEM-encoded TLS private key (TCP protocols) |
| `tls_insecure` | bool | false | Skip TLS verification (QUIC, not recommended) |
| `encryption_enabled` | bool | false | Enable E2EE/SSE encryption |
| `encryption_key` | string | "" | 64-char hex (256-bit) master encryption key |
| `encryption_tenant` | string | "default" | Tenant ID for key derivation |

#### Scenario: Encryption enabled without key
- **GIVEN** `encryption_enabled = true` and `encryption_key` is empty
- **WHEN** the server starts
- **THEN** the server fails to start with error: "encryption_enabled is true but
  encryption_key is not set"

#### Scenario: Invalid encryption key length
- **GIVEN** `encryption_key` is not a 64-character hex string
- **WHEN** the server starts
- **THEN** the server fails to start with error: "encryption_key must be a
  64-character hex string (256-bit key)"

### Requirement: Backward Compatibility (All Phases)

When `encryption_enabled = false` (default) and `tls_cert`/`tls_key` are empty
(default), all protocols SHALL behave exactly as before — plaintext TCP/QUIC
with plaintext auth token. No existing deployment breaks on upgrade.
