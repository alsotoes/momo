---
title: "S3 Auth: SigV4, Presigned URLs, and Key Decoupling"
date: 2026-08-11T23:11:16Z
draft: false
post_type: architecture
tags: [go, s3, sigv4, auth, sentinel]
categories: [s3]
summary: "Query-string SigV4 for presigned URLs, mandatory X-Amz-Date freshness, and decoupling gateway access/secret keys from the native auth token."
artifacts:
  - {type: pr, id: "789"}
  - {type: pr, id: "791"}
  - {type: pr, id: "885"}
  - {type: spec, path: openspec/changes/signed-payload-and-sse}
related:
  - 008-s3-gateway-core
  - 011-s3-https-tls-enforcement
  - 015-sentinel-security-audit
  - 039-signed-payload-sse-s3
  - 040-aws-chunked-streaming
---
SigV4 — AWS Signature Version 4 — is the request-signing scheme every S3 client
speaks. It is more than "an auth scheme": the client derives a signing key from
its secret, canonicalizes the request (method, path, headers, payload hash), and
signs that canonical form. Done wrong, the scheme becomes a replay machine — an
attacker who captures one valid request can send it again and again. This post
is about making signing correct, honest, and separate from momo-native auth.

## The Real Problem

Authentication is the one place where a small implementation mistake is a full
compromise rather than a bug. Three specific things worried us.

First, **presigned URLs**. A presigned URL is a normal request whose signature
travels in the query string instead of an `Authorization` header, so the URL
itself grants time-limited, scoped access without ever exposing the secret key.
Real tools depend on this: SDKs generate them, `curl` fetches them, and `rclone`
uses them for expiring links. If the gateway cannot verify a query-string
signature, half the ecosystem cannot use it.

Second, **freshness**. A signature is only as safe as the request it is bound to.
If the timestamp can be omitted or ignored, a captured signature stays valid
forever. Freshness — rejecting requests whose signed time is too far from the
server's clock — is what turns a signature into a short-lived credential.

Third, **key separation**. The gateway's S3 access key and the momo-native auth
token were originally the same secret. That meant leaking one surface exposed the
other, and it made two very different lifetimes impossible to reason about.

## What We Changed

- **Query-string SigV4 for presigned URLs.** The gateway verifies signatures
  carried as `X-Amz-Credential`, `X-Amz-Signature`, and `X-Amz-Date` in the query
  string, so temporary links work exactly as SDKs expect.
- **`X-Amz-Date` is required.** We removed a dead fallback path that accepted a
  signature with no freshness check at all — a replay hole in the making.
- **Freshness applies to native auth too.** The plaintext challenge-response path
  is now checked against clock skew, not just the S3 path.
- **Encoding bounds by runes, not bytes.** Percent-escaping a signing key must
  count characters, not bytes, or keys longer than about a kilobyte containing
  multi-byte UTF-8 sign incorrectly. Bounding by runes fixed that.
- **Gateway keys are decoupled from the native token.** Two keys, two lifetimes:
  the S3 access/secret pair and the momo-native token no longer double as each
  other.

## The Trade-off

Requiring freshness means a client with a badly skewed clock is rejected. That is
the correct failure — a loud `RequestTimeTooSkewed` beats silently accepting a
replayable request — but it does mean operators must keep their clocks
synchronized. Decoupling the keys also means two credentials to provision and
rotate instead of one; we accepted that cost because shared secrets are exactly
the kind of coupling a security review flags.

## ⚡ Bolt & 🛡 Sentinel

🛡 **Sentinel.** Auth is where Sentinel thinking concentrated hardest: replay
protection, freshness, least-privilege key separation, and honest signing. The
`signed payload` posture — signing the payload hash rather than trusting a
declared value — is part of the same story.

⚡ **Bolt.** Signing is compute-regular: stable, allocation-light hashing, with
deadlines amortized across header reads rather than re-derived per field.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Gateway core: [008](008-s3-gateway-core.md). Transport security:
  [011](011-s3-https-tls-enforcement.md). Audit deep-dive:
  [015](015-sentinel-security-audit.md). Signed-payload and SSE:
  [039](039-signed-payload-sse-s3.md), [040](040-aws-chunked-streaming.md).
- Auth spec and pull requests: `openspec/changes/signed-payload-and-sse`
  (PRs #789, #791, #885).
- Pentest guidance: `docs/PENTESTING.md`.