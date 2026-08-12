> GitHub Issue URL: https://github.com/alsotoes/momo/issues/775

# S3 — Inbound TLS Enforcement Specification

## Purpose
This specification requires TLS on the `s3-tcp` inbound gateway to prevent silent cleartext serving of S3 REST requests (object payloads, SigV4 headers, credentials material) when no TLS certificate is configured.

## ADDED Requirements

### Requirement: s3-tcp TLS enforcement on listen
`Listen` SHALL reject `s3-tcp` without a configured TLS certificate unless `tls_insecure = true` is explicitly set. When rejected, the error SHALL carry `EINVAL`.

#### Scenario: s3-tcp without TLS rejected by default
- **GIVEN** `protocol = s3-tcp` with no `tls_cert`/`tls_key` and `tls_insecure = false` (default)
- **WHEN** `Listen` is called
- **THEN** it returns an `EINVAL` error

#### Scenario: s3-tcp with TLSInsecure accepted with warning
- **GIVEN** `protocol = s3-tcp` with no `tls_cert`/`tls_key` and `tls_insecure = true`
- **WHEN** `Listen` is called
- **THEN** it succeeds and a prominent warning is logged

#### Scenario: s3-tcp with TLS config accepted
- **GIVEN** `protocol = s3-tcp` with `tls_cert`/`tls_key` configured
- **WHEN** `Listen` is called
- **THEN** it succeeds with a TLS-wrapped listener

### Requirement: momo-tcp unchanged
The `momo-tcp` protocol SHALL NOT be affected by the s3-tcp enforcement — it carries its own authentication and does not require TLS. `Listen` for `momo-tcp` without TLS SHALL succeed as before.

### Requirement: QUIC self-signed fallback warning
When `momo-quic` or `s3-quic` falls back to a self-signed certificate (no `tls_cert`/`tls_key` configured), a SHALL log a warning that the connection is encrypted but the server identity is unauthenticated.

