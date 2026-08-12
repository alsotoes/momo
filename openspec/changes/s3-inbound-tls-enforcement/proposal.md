# Change: S3 — require TLS (or explicit insecure) for the s3-tcp inbound gateway
**Related Issues:**
- https://github.com/alsotoes/momo/issues/775

## Why
The `s3-tcp` inbound gateway serves S3 REST requests over raw TCP. Without a configured TLS cert/key pair, the entire S3 exchange — object payload, SigV4 headers, credentials material — travels in cleartext. Only `s3-quic` is encrypted-by-default (TLS 1.3). For "real end-to-end encryption" the inbound S3 gateway must either require TLS or loudly refuse to serve sensitive traffic without it.

## What Changes
- **`s3-tcp` `Listen` enforces TLS.** When `f.tlsConfig == nil` (no `tls_cert`/`tls_key` configured):
  - If `tls_insecure` is not set → return `EINVAL` error ("s3-tcp requires TLS").
  - If `tls_insecure = true` → log prominent warning and accept cleartext.
- **`momo-tcp` unchanged.** The momo protocol carries its own authentication and does not require TLS.
- **QUIC self-signed fallback warning added.** For `momo-quic`/`s3-quic` without configured certs, log a warning that the connection is encrypted but the server identity is unauthenticated.
- **`docs/PROTOCOL.md` updated.** Documents the inbound gateway TLS requirements per protocol.

## Non-Goals
- No change to `momo-tcp`/`momo-quic` clients (their auth is independent of TLS).
- No change to the outbound S3 storage enforcement (already handled in #774).

