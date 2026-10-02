---
title: 'S3 and Inbound TLS: HTTPS-by-Default Enforcement'
date: 2026-08-12 07:23:29+00:00
draft: false
post_type: architecture
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
- 033-s3-501-discipline-bucket-config
---
TLS — Transport Layer Security — is the protocol that encrypts and authenticates
a network connection; HTTPS is simply HTTP carried over TLS. "Plaintext" means a
connection with no encryption at all, where anyone on the path can read or alter
the bytes. This post is about a small policy change with a large blast radius:
making TLS the default for the S3 gateway and for inbound daemon traffic, so that
silence no longer means "insecure".

## The Real Problem

The gateway shipped with an HTTP listener that just worked. That convenience was
the danger: a client could send object data and credentials in the clear and
never be told. Worse, because plaintext was the default, an attacker on the
network could **downgrade** a connection — strip or block the TLS attempt and
watch the client fall back to HTTP without complaint. A protocol that silently
permits weakness will be silently weakened.

We also had to be honest about scope. momo did not (yet) have an authentication
story of its own, which meant the wire was the last line of defence. If the store
goes to great lengths to protect bytes at rest, letting those same bytes cross
the network unencrypted is a contradiction.

## The Solution: No Silent Plaintext

Two changes enforced the rule:

- **The S3 gateway requires HTTPS.** A plain HTTP request is rejected unless the
  operator has explicitly opted into an insecure listener. A client that "just
  works" over HTTP must now opt into weakness loudly.
- **Inbound daemon transport requires TLS.** Before the daemon accepts
  replication or transport connections, the peer must complete a TLS handshake —
  or the operator must set an explicit insecure flag.

The key word is *explicit*. Insecure mode did not disappear; it became something
you have to ask for by name. That turns an accident into a decision.

## The Trade-off

- **Cost.** The daemon has no certificate-management flow — no automated
  certificate issuance or renewal — so TLS defaults to a self-signed certificate
  or one the operator supplies. Clients must be told to trust it, which is real
  operational friction.
- **Win.** Silent downgrade attacks and on-wire sniffing are structurally
  removed. The insecure flag still exists, but it can only be set deliberately;
  it can no longer be reached by omission.

We chose the friction. A self-signed certificate that a client explicitly trusts
is still far better than an encryption-free connection nobody chose.

## ⚡ Bolt & 🛡 Sentinel

🛡 **Sentinel.** This is a pure Sentinel call: the wire must not leak what the
store protects elsewhere. Combined with SigV4 freshness
([010](010-s3-auth-presigned-sigv4.md)), the gateway is both replay-safe and
confidentiality-safe.

⚡ **Bolt.** The TLS decision sits on the control plane, not the data path. Once
a connection is established, the same allocation-light streaming rules apply as
before; enforcing TLS does not add per-object overhead.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Gateway core: [008](008-s3-gateway-core.md) →
  [010: SigV4](010-s3-auth-presigned-sigv4.md) → this post →
  [015: Security Audit](015-sentinel-security-audit.md).
- Daemon transport TLS/QUIC: [003](003-transport-tcp-to-quic.md).
- Gateway and inbound TLS specs: `openspec/changes/s3-https-enforcement`,
  `openspec/changes/s3-inbound-tls-enforcement` (PRs #792, #793).
- Configuration keys and pentest guidance: `docs/CONFIGURATION.md`,
  `docs/PENTESTING.md`.