---
title: 'The Sentinel Sweep: An Audit-Driven Security Journey'
date: 2026-08-04 00:47:24+00:00
draft: false
post_type: issue
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
On 2026-08-04 we pointed a mechanized audit at an early momo and let it run deep:
76 findings, filed as a chain of issues from #593 onward. It was uncomfortable. It
was also the moment the **🛡 Sentinel mindset** was born — the idea that a security
flaw is not a special category of work to schedule later, but a first-class
engineering item with a reproducible failure, an auditable fix, and a regression
test that keeps it fixed.

## What the sweep found

The findings clustered into a handful of classes, each a textbook way for
untrusted input to cross a boundary it should never cross.

- **CRLF injection.** `CRLF` stands for carriage-return / line-feed, the two bytes
  that terminate a line in HTTP and many text protocols. If attacker-controlled
  bytes containing `\r\n` reach a header or a log line, they can forge a new line —
  splitting one response into two, or smuggling fake fields into a log. We found
  untrusted bytes flowing into metadata and metrics paths.
- **HTTP request smuggling.** This is the classic ambiguity attack: when a request
  carries both a `Transfer-Encoding` header and a `Content-Length` header, two
  servers in the chain can disagree about where the request ends. An attacker
  crafts a request that the front gateway reads one way and the backend reads
  another, letting them "smuggle" a second request past the gateway's checks. We
  found this in the S3 gateway, and it was the most severe issue of the sweep.
- **Path traversal.** When a filename is used to build a filesystem path, a value
  containing `..` can climb out of the intended directory and read or write
  somewhere it should not. Our first guard flagged *false positives* — filenames
  that merely contained `..` as part of a longer name were rejected even though
  they were harmless. The fix was to flag only a **complete path component** that
  is exactly `..`, not any occurrence of the characters.
- **Resource leaks.** The long tail: request bodies never closed, a wait-group
  counter incremented after the wait had already begun, and a lease that could
  reach zero quorum during a network partition. Individually minor; collectively a
  slow bleed of file descriptors and goroutines that would eventually take a node
  down.

## Process: issuance → regression → hardening

The value was not the list; it was the discipline applied to each entry. Every
finding got a reproducible acceptance case first — a test that fails against the
vulnerable code — then a fix, then, where it made sense, a regression contract test
so the exact class of bug could never return unnoticed. The offensive toolkit we
used (fuzzing for path traversal plus hand-written exploits) was kept alongside the
code and documented, so the next audit starts from where this one ended rather than
from scratch.

## The mindset distilled

Three rules fell out of the sweep and became permanent:

- **Fail closed, loudly.** When input is suspicious, refuse it with an honest error
  rather than papering over it with a fake success. A server that returns "not
  implemented" for something it cannot safely do is more trustworthy than one that
  returns a 200 and hopes. We map internal errors to POSIX syscall constants so
  callers get a truthful, machine-readable reason.
- **Trust invariants are never bypassed.** The checks that matter — CRLF
  sanitization, crypto, integrity verification — live in the auditable core and
  cannot be routed around by a plugin or an alternate code path. If a fast path
  skips a trust check, it is not a fast path, it is a vulnerability.
- **Zero-crash handling.** Panics in request handling become recovered errors, not
  process death; unbounded readers get bounds; every goroutine is verified not to
  leak. A storage node that crashes on malformed input is a denial-of-service
  waiting to happen.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Pentest toolkit and write-ups: `docs/PENTESTING.md`, `pentest/`.
- Tracking issues: [#593](https://github.com/alsotoes/momo/issues/593),
  [#811](https://github.com/alsotoes/momo/issues/811),
  [#859](https://github.com/alsotoes/momo/issues/859),
  [#889](https://github.com/alsotoes/momo/issues/889).
- The trust-critical core is governed by the Sentinel constraints in
  `openspec/config.yaml`.
- Sibling posts: [007: At-Rest Integrity and GC](007-at-rest-integrity-and-gc.md),
  [010: S3 Auth, Presigned URLs, and SigV4](010-s3-auth-presigned-sigv4.md),
  [011: S3 HTTPS/TLS Enforcement](011-s3-https-tls-enforcement.md),
  [013: Client-Held E2EE](013-e2ee-envelope-encryption.md),
  [014: Confidential Dedup via OPRF](014-confidential-dedup-oprf.md),
  [017: Scatter-Gather Lease Quorum](017-scatter-gather-lease-quorum.md),
  [026: Metrics and Observability](026-metrics-observability.md),
  [037: Zero-Crash Hardening](037-zero-crash-hardening-patterns.md),
  [046: Auto-Trace Deduplication](046-auto-trace-dedup.md).
