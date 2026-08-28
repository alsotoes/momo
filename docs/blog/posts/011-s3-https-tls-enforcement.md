---
title: 'S3 and Inbound TLS: HTTPS-by-Default Enforcement'
date: 2026-08-12 07:23:29+00:00
draft: false
tags:
- go
- s3
- tls
- https
- sentinel
categories:
- s3
summary: 'The S3 gateway turned HTTPS-or-explicit-insecure into the default: no silent
  plaintext for the gateway or inbound tcp/quic.'
artifacts:
- type: pr
  id: '792'
- type: pr
  id: '793'
- type: spec
  path: openspec/changes/s3-https-enforcement
- type: spec
  path: openspec/changes/s3-inbound-tls-enforcement
related:
- 008-s3-gateway-core
- 010-s3-auth-presigned-sigv4
- 003-transport-tcp-to-quic
- 015-sentinel-security-audit
---
 S3 and Inbound TLS: HTTPS-by-Default Enforcement

Two hardening PRs (#792 #793) enforced the rule: **no silent plaintext** on the
S3 path.

- **S3 gateway** (#792): requires HTTPS — or an explicit `insecure` opt-in —
  for the S3-compatible endpoint. A client that "just works" over HTTP must opt
  into weakness loudly (`docs/CONFIGURATION.md`, `[storage]`/gateway TLS keys).
- **Inbound s3-tcp** (#793): TLS (or explicit `tls_insecure=true`) is required
  before the daemon accepts replication/transport connections.

This is a pure **Sentinel** call: momo has no auth/TLS by design (*yet*), so the
*wire* must not leak the bytes the store is protecting elsewhere. Combined with
SigV4 freshness ([010](010-s3-auth-presigned-sigv4.md)) the gateway is
replay-safe *and* confidentiality-safe.

## The tradeoff, stated

- **Cost**: this daemon lacks certificate management (no Let's Encrypt flow), so
  TLS defaults to self-signed or operator-supplied certs.
- **Win**: silent downgrade attacks and on-wire sniffing are structurally
  removed; the pentest docs
  (`docs/PENTESTING.md`) and the hardened test configs all carry
  `tls_insecure=true` explicitly — the flag is still crackable, but only by
  choice.

## Backup links

Transport-level TLS/QUIC fed into the same posture — see
[003](003-transport-tcp-to-quic.md) for the daemon-transport TLS face, and
[015](015-sentinel-security-audit.md) for the audit context.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

[008](008-s3-gateway-core.md) → [010](010-s3-auth-presigned-sigv4.md) → this post →
[015](015-sentinel-security-audit.md).