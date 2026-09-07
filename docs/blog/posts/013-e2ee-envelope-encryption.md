---
title: "Client-Held E2EE: Envelope Encryption Real"
date: 2026-08-11T16:36:53Z
draft: false
tags: [go, encryption, e2ee, sentinel]
categories: [encryption]
summary: "Envelope encryption with a client-held key: the server stores ciphertext and can't read it — across S3 and native transports."
artifacts:
  - {type: pr, id: "779"}
  - {type: pr, id: "781"}
  - {type: spec, path: openspec/changes/add-e2e-encryption}
related:
  - 014-confidential-dedup-oprf
  - 006-pluggable-storage-backends
  - 015-sentinel-security-audit
---
# Client-Held E2EE: Envelope Encryption Real

The strongest privacy claim momo makes: **the server can't read your data**.
Delivered twice in one day — once for S3 (#779), once for native transports
(#781) — as client-side **envelope encryption**.

## Envelope encryption in one paragraph

A random **content key (CEK)** encrypts the object bytes; a wrapping
**key-encryption key (KEK)**, held only by the client, encrypts the CEK. The
server stores ciphertext **plus** the wrapped CEK and never sees the KEK. Losing
the KEK means the ciphertext is semantically dead — that's the point.

## Architecture: under the store, transparent to it

The EncryptedBlobStore wraps the pluggable Store seam
([006](006-pluggable-storage-backends.md)):

```
Handler PUT → EncryptBlobStore → (wrapped CEK, ciphertext) → Store backend
Handler GET → Store backend → (wrapped CEK, ciphertext) → Decrypt w/ client KEK
```

- CAS dedup neutralizes on ciphertext, as designed (see
  [014](014-confidential-dedup-oprf.md) for the *confidential* version).
- Works across **both** transports: S3 gateway (#779) and native
  momo-tcp/quic (#781) — same Store, same envelope, one story.
- SSE-C/KMS "honest posture" later hardened what the server *claims* vs *does*
  (#791, documented in `docs/momofs/IMPLEMENTATION`-adjacent SSE notes — tagged
  `planned`/actual per implementation state).

## 🛡 Sentinel lens

- **Goroutine-leak fix**: EncryptedBlobStore previously leaked a goroutine on
  `PutBlob` error during context cancellation (issue #603) — patched.
- Streaming encryption carries an integrity footer so truncation is detectable
  (issue #600), closing the kill-the-tail attack.
- Rule 74: crypto stays in the auditable core, never bypassed by a seam.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Confidential dedup: [014](014-confidential-dedup-oprf.md). Underlying seam:
[006](006-pluggable-storage-backends.md). Audit: [015](015-sentinel-security-audit.md).