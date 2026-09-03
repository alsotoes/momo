# 0040-s3-inbound-tls-enforcement

## Status
Proposed

## Confidence
Medium

## Context
The `s3-tcp` inbound gateway serves S3 REST requests over raw TCP. Without a configured TLS cert/key pair, the entire S3 exchange — object payload, SigV4 headers, credentials material — travels in cleartext. Only `s3-quic` is encrypted-by-default (TLS 1.3). For "real end-to-end encryption" the inbound S3 gateway must either require TLS or loudly refuse to serve sensitive traffic without it.

## Decision
- s3-tcp TLS enforcement on listen: `Listen` SHALL reject `s3-tcp` without a configured TLS certificate unless `tls_insecure = true` is explicitly set. When rejected, the error SHALL carry `EINVAL`.
- momo-tcp unchanged: The `momo-tcp` protocol SHALL NOT be affected by the s3-tcp enforcement — it carries its own authentication and does not require TLS. `Listen` for `momo-tcp` without TLS SHALL succeed as before.
- QUIC self-signed fallback warning: When `momo-quic` or `s3-quic` falls back to a self-signed certificate (no `tls_cert`/`tls_key` configured), a SHALL log a warning that the connection is encrypted but the server identity is unauthenticated.

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
- Spec: openspec/changes/s3-inbound-tls-enforcement/
- Blog: docs/blog/posts/...md
