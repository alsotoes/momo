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
SigV4 isn't just "an auth scheme" — done wrong it's a replay machine. This
post covers how the gateway made signing correct, honest, and separate from
momo-native auth.

## Query-string SigV4 for presigned URLs (#789)

Presigned (query-string) Signatures grant temporary, scoped access without
sharing the secret: `X-Amz-Credential`/`X-Amz-Signature`/`X-Amz-Date` in the
query. Real tools (SDKs, `curl`, `rclone copy` with expiring links) depend on
this.

## Honest signing (with help from the audit)

- `X-Amz-Date` is now **required**; a dead sigv4 fallback path was removed
  (#881) — a replay-without-freshness hole in the making.
- Timestamp freshness validation applies to the plaintext auth path too — the
  native challenge-response is checked against clock skew (#886).
- `sigv4Escape` bounds by **runes**, not bytes, so 1024+ multi-byte UTF-8 keys
  sign correctly (#884).
- #885 **decouples** gateway SigV4 access/secret keys from the momo native auth
  token. Previously the token served as *both* — leaking through one surface
  exposed the other. Decision: two keys, two lifetimes.

## 🛡 Sentinel lens

Auth is where Sentinel thinking concentrated most: replay protection,
freshness, least-privilege key separation, and honest signing
(`SIGNED_PAYLOAD` posture in #791). The pentest guidance lives in
`docs/PENTESTING.md`; the auth hardening crosses into
[015](015-sentinel-security-audit.md).

## ⚡ Bolt lens

Signing is compute-regular; the canonical Bolt rule applies — stable,
allocation-light hashing, with deadlines amortized across header reads.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Gateway core: [008](008-s3-gateway-core.md). Transport security:
[011](011-s3-https-tls-enforcement.md). Audit deep-dive:
[015](015-sentinel-security-audit.md).