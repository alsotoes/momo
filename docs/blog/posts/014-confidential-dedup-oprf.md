---
title: "Confidential Dedup via Threshold OPRF — and Auth Lockout"
date: 2026-08-14T00:26:56Z
draft: false
post_type: architecture
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
Client-held end-to-end encryption bought us zero trust at the server, but it
created a paradox. Deduplication — storing one physical copy of a file that many
users upload — normally works by hashing the content: identical files produce
identical hashes, and the store keeps one copy. But if only the client can see
content, and every object is encrypted under a fresh random key, how do we dedup
without leaking which files users have?

## The paradox and the fix

The naive approach, unidirectional encryption of content, has a quiet flaw: it
leaves a stable hash of the content that the server can compute. That stable hash
is a fingerprint. An attacker holding a candidate file can hash it and ask "does
this blob match the target file?" — a **content-confirmation attack** — turning
the dedup index into an oracle that confirms file possession. Encryption was
supposed to prevent exactly that.

We wanted the opposite property: the server should be able to recognize that two
uploads are identical without ever learning *what* they are. The tool for that is
a **threshold OPRF**.

An **OPRF** (oblivious pseudo-random function) is a two-party protocol: the
server holds a secret key and can evaluate a keyed function on the client's input,
but it learns nothing about the input; the client gets the function's output but
cannot compute it alone, because it does not have the server's key. **Blinding** is
the trick that hides the input: the client transforms its value with a random
mask before sending it, so the server sees an unrecognizable number, evaluates on
that, and returns a result the client can unblind. **Threshold** means the server
is not one machine but a quorum of peers that must cooperate — no single node
holds the whole key, so no single compromised node can evaluate the function on
its own.

The result is a dedup signal that is **not** a deterministic function of content
alone. Two clients uploading the same file agree on the OPRF output and dedup
correctly, but the server sees only blinded values and can never confirm a
candidate file. We get **confidential dedup without content-hash disclosure**, and
without forcing every upload to break encryption.

The protocol shape is:

```
client → OPRF(blind(password[content])) → server (threshold quorum) → PRF output
```

## Supporting security hardening

The same hardening wave taught authentication to fail **adaptively** rather than
either accepting everything or locking the world out. Two behaviors:

- **Failed-auth backoff and temporary lockout.** Repeated wrong credentials
  trigger a growing delay and then a short lockout, which throttles
  credential-stuffing without permanently denying the legitimate holder. The
  lockout is temporary by design, so a burst of guesses cannot permanently
  disable an account.
- **Debounced, adaptive thresholds.** The backoff parameters respond to observed
  pressure, modeled on the same adaptive patterns we use for gossip and peer
  quality elsewhere in the system, rather than a fixed constant that is either
  too loose under attack or too tight under normal load.

## 🛡 Sentinel lens

An OPRF is a private-set-membership protocol, and those are subtle to get right:
a single implementation mistake can turn the oracle back on. That is why it lives
**in the auditable core**, the small set of trust-critical code that is reviewed
as a unit and never bypassed by a seam or a plugin. We pair it with an honest
server-side-encryption and key posture, and with replay protection on the request
path, so the protocol is not the only line of defense.

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Specs: `openspec/changes/secure-e2ee-confidential-dedup`,
  `openspec/changes/add-adaptive-auth-backoff`.
- Pull requests: [#819](https://github.com/alsotoes/momo/pull/819),
  [#826](https://github.com/alsotoes/momo/pull/826).
- Sibling posts: [013: Client-Held E2EE](013-e2ee-envelope-encryption.md),
  [015: The Sentinel Sweep](015-sentinel-security-audit.md),
  [017: Scatter-Gather Lease Quorum](017-scatter-gather-lease-quorum.md),
  [018: Adaptive Peer Quality](018-adaptive-scaling-peer-quality.md),
  [010: S3 Auth, Presigned URLs, and SigV4](010-s3-auth-presigned-sigv4.md).
