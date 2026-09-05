---
title: 'The Sentinel Sweep: An Audit-Driven Security Journey'
date: 2026-08-04 00:47:24+00:00
draft: false
tags:
- go
- security
- sentinel
- pentest
- crlf
categories:
- encryption
summary: A 76-deep audit (authed via Sentinel) surfaced CRLF injection, smuggling,
  traversal and path leaks — the pattern that forged momo's security mindset.
artifacts:
- type: issue
  id: '593'
- type: issue
  id: '811'
- type: issue
  id: '859'
- type: issue
  id: '889'
- type: doc
  path: docs/PENTESTING.md
related:
- 013-e2ee-envelope-encryption
- 011-s3-https-tls-enforcement
- 010-s3-auth-presigned-sigv4
- 007-at-rest-integrity-and-gc
- 014-confidential-dedup-oprf
- 046-auto-trace-dedup
- 017-scatter-gather-lease-quorum
- 026-metrics-observability
- 037-zero-crash-hardening-patterns
---
he Sentinel Sweep: An Audit-Driven Security Journey

On 2026-08-04 a mechanized audit — issue **#593 → #668**, 76 findings — was
filed against an early momo. This is the origin of the **🛡 Sentinel mindset**:
security flaws treated as first-class engineering items with auditable fixes,
not optional polish.

## What the sweep found (headline classes)

- **CRLF injection** in `ReceiveMetadata` and `getMetadata` (HIGH, #859/#889):
  untrusted bytes reaching metrics/log paths.
- **HTTP Request Smuggling** (CRITICAL, #811) in the S3 gateway — the classic
  `Transfer-Encoding`/`Content-Length` ambiguity leapfrogged gateways.
- **Path traversal** in `RawBlobStore` (HIGH, #838) — `HasPathTraversalChars`
  originally flagged *false positives* for filenames containing `..` (fixed #874
  to flag only *complete* components).
- **Resource leaks**: `req.Body` never closed, `wg.Add` racing `wg.Wait`, lease
  zero-quorum on partition, an earthworm of medium/low issues that became the
  batch-in-one-day hardening wave.

## Process: issuance → regression → hardening

Each finding got: a reproducible acceptance case (e.g. CRLF rejection test in
`acdc76db`), a fix PR, and where applicable a regression contract test. The
pentest toolkit lived in `pentest/` (DotDotPwn fuzzing + Python exploits — 9
CVEs found) documented at `docs/PENTESTING.md`.

## The mindset distilled (Rule 74 / CONSTRAINTS)

- **Fail closed, loudly** — 501 over fake 200; verify before trusting;
  map errors to syscall constants so callers get POSIX truth.
- **Core trust invariants never seam-bypassed** — CRLF/crypto/integrity stay in
  the auditable core.
- **Zero-crash pattern** — panics in handling become recover(); unbounded
  readers bounded; every handle is goleak-verified.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Encryption posture: [013](013-e2ee-envelope-encryption.md). Wire hardening:
[011](011-s3-https-tls-enforcement.md) · [010](010-s3-auth-presigned-sigv4.md).
Integrity: [007](007-at-rest-integrity-and-gc.md).