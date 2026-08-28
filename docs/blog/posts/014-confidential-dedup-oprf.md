---
title: "Confidential Dedup via Threshold OPRF — and Auth Lockout"
date: 2026-08-14T00:26:56Z
draft: false
tags: [go, crypto, oprf, e2ee, sentinel]
categories: [encryption]
summary: "Threshold-OPRF lets E2EE cas blobs dedup without revealing content hashes — plus adaptive failed-auth backoff and lockout."
artifacts:
  - {type: pr, id: "819"}
  - {type: pr, id: "826"}
  - {type: spec, path: openspec/changes/secure-e2ee-confidential-dedup}
  - {type: spec, path: openspec/changes/add-adaptive-auth-backoff}
related:
  - 013-e2ee-envelope-encryption
  - 017-scatter-gather-lease-quorum
  - 015-sentinel-security-audit
---
# Confidential Dedup via Threshold OPRF — and Auth Lockout

E2EE ([013](013-e2ee-envelope-encryption.md)) created a paradox: if only the
client can see content, how do you dedup without leaking which files users have?

## The paradox and the fix

Naive UEC (unidirectional encryption of content) leaks a stable hash that
enables content-confirmation attacks ("does this blob match the target file?").
**Threshold-OPRF** (oblivious pseudo-random function, #819) breaks that: the
server applies the OPRF only when enough peer quorum participates —
**confidential dedup without content-hash disclosure** and without
per-second-upload broken encryption.

Protocol shape: `client → OPRF(blind password[content]) → server (threshold
quorum) → PRF output` — a dedup signal that is **not** a deterministic function
of content alone. Ratified in
`openspec/changes/secure-e2ee-confidential-dedup/`.

## Supporting security hardening (#826)

The same hardening wave taught auth to fail **adaptively**:

- failed-auth **backoff** + temporary **lockout** (and "auth/lockout" awareness)
  — throttles credential-stuffing without a permanent DoS for the holder.
- Debounced / adaptive settings modeled on the gossip/streaming adaptive
  patterns ([018](018-adaptive-scaling-peer-quality.md)).

## 🛡 Sentinel lens

The OPRF is a private-set-membership protocol — subtle to get right. It was
kept **in the auditable core** (Rule 74), with honest SSE/keys posture from
[010](010-s3-auth-presigned-sigv4.md) and replay protection referenced there.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Parent crypto: [013](013-e2ee-envelope-encryption.md). Adaptive peers:
[018](018-adaptive-scaling-peer-quality.md). Audit: [015](015-sentinel-security-audit.md).