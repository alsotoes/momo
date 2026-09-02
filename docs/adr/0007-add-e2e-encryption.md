# 0007-add-e2e-encryption

## Status
Proposed

## Confidence
Medium

## Context


## Decision
- Transport TLS Encryption (Phase 1, Resolves CVE-009 #546): All TCP-based wire protocols (`momo-tcp`, `s3-tcp`) SHALL support TLS 1.2/1.3 when `tls_cert` and `tls_key` are configured. QUIC protocols (`momo-quic`, `s3-quic`) already use TLS 1.3 via QUIC but SHALL default `InsecureSkipVerify` to `false`, requiring either a CA certificate (`ca_cert`) or explicit opt-in (`tls_insecure = true`).
- Challenge-Response Authentication (Phase 1, Resolves CVE-009 #546): The authentication token SHALL never be transmitted in plaintext. Instead, the server SHALL send a 32-byte cryptographically secure random nonce, and the client SHALL respond with `HMAC-SHA256(auth_token, nonce)`. The server verifies the HMAC without ever receiving the token.
- AES-GCM-256 Content Encryption (Phase 2): The `src/crypto` package SHALL provide authenticated symmetric encryption using AES-GCM-256 for both in-memory and streaming operations.
- Convergent Encryption for Dedup (Phase 2, Gap 2): The system SHALL support convergent encryption where the encryption key is derived from the content itself, enabling deduplication of identical plaintext across different clients while maintaining confidentiality from the server.
- Per-Tenant Key Derivation (Phase 5, Gap 4): The system SHALL derive a unique 256-bit encryption key per tenant using HKDF-SHA256 from a master key, ensuring cryptographic isolation between tenants.
- E2EE momo Protocol — Content + Metadata Encryption (Phase 3, Gaps 1 & 3): For `momo-tcp` and `momo-quic`, the client SHALL encrypt both file content AND metadata (filenames, virtual paths) before transmitting to the server. The server stores only encrypted content and encrypted names, achieving zero-knowledge storage.
- SSE S3 Fallback — Server-Side Encryption at Rest (Phase 4): For `s3-tcp` and `s3-quic`, the server SHALL encrypt data at rest using an `EncryptedBlobStore` decorator. The server sees plaintext transiently during PUT/GET (unavoidable for S3-compatible protocols).
- Configuration (All Phases): The configuration SHALL support the following new fields in the `[global]` section: | Field | Type | Default | Description | |-------|------|---------|-------------| | `tls_cert` | string | "" | Path to PEM-encoded TLS certificate (TCP protocols) | | `tls_key` | string | "" | Path to PEM-encoded TLS private key (TCP protocols) | | `tls_insecure` | bool | false | Skip TLS verification (QUIC, not recommended) | | `encryption_enabled` | bool | false | Enable E2EE/SSE encryption | | `encryption_key`...
- Backward Compatibility (All Phases): When `encryption_enabled = false` (default) and `tls_cert`/`tls_key` are empty (default), all protocols SHALL behave exactly as before — plaintext TCP/QUIC with plaintext auth token. No existing deployment breaks on upgrade.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Partial
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-e2e-encryption/
- Blog: docs/blog/posts/...md
