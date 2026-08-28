---
title: "Momo Engineering Journal"
description: "The momo journey — research, architecture decisions, and changes behind a distributed object store written in Go."
date: 2025-09-09T20:10:05Z
draft: false
categories: [origin]
summary: "Index of the momo engineering journal covering genesis through the durable, mountable, S3-compatible object store."
---
# Momo Engineering Journal

The distilled journey of **momo** — from a file-replication playground to a
distributed, content-addressed object store with an S3 gateway, P2P gossip
ring, quorum durability, and a FUSE/POSIX mount. Every post documents **what was
built** (verified against `src/`), **why** (architecture decisions), and the
⚡ Bolt / 🛡 Sentinel tradeoffs that shaped it.

## The arc

1. **Origin** — [001: from playground to object store](posts/001-origin-and-genesis.md)
2. **Transport** — [002: replication strategies](posts/002-replication-strategies-polymorphic.md) · [003: TCP → QUIC](posts/003-transport-tcp-to-quic.md)
3. **Storage core** — [004: CAS](posts/004-cas-content-addressable-store.md) · [005: CRUSH](posts/005-crush-placement.md) · [006: pluggable backends](posts/006-pluggable-storage-backends.md) · [007: at-rest integrity + GC](posts/007-at-rest-integrity-and-gc.md)
4. **S3 gateway** — [008: core](posts/008-s3-gateway-core.md) · [009: multipart + breadth](posts/009-s3-multipart-and-breadth.md) · [010: SigV4 + presigned](posts/010-s3-auth-presigned-sigv4.md) · [011: HTTPS/TLS](posts/011-s3-https-tls-enforcement.md) · [012: checksums](posts/012-s3-integrity-checksums.md)
5. **Encryption & security** — [013: E2EE](posts/013-e2ee-envelope-encryption.md) · [014: confidential dedup + OPRF](posts/014-confidential-dedup-oprf.md) · [015: Sentinel audit](posts/015-sentinel-security-audit.md)
6. **P2P** — [016: gossip/SWIM](posts/016-p2p-gossip-swim.md) · [017: scatter-gather + lease](posts/017-scatter-gather-lease-quorum.md) · [018: adaptive scaling](posts/018-adaptive-scaling-peer-quality.md)
7. **Durability** — [019: R1 failure domains](posts/019-r1-failure-domain-placement.md) · [020: R2 degraded-read/self-heal](posts/020-r2-degraded-read-self-heal.md) · [021: R3 write durability](posts/021-r3-write-durability-quorum.md)
8. **momofs** — [022: POSIX core](posts/022-momofs-posix-core.md) · [023: FUSE transport](posts/023-momofs-fuse-transport.md)
9. **Performance & governance** — [024: Bolt engineering](posts/024-bolt-performance-engineering.md) · [025: benchstat gate](posts/025-benchmark-benchstat-gate.md) · [026: metrics](posts/026-metrics-observability.md) · [027: AI review + spec-first](posts/027-governance-ai-review-spec-first.md)
10. **Forward** — [028: roadmap + research](posts/028-roadmap-and-research.md)

## Conventions

- Post `date` = anchor issue/PR `createdAt` (or earliest code/plan commit when
  no issue exists). See [README.md](README.md).
- ⚡ `bolt` / 🛡 `sentinel` tags mark posts where performance/security drove the
  design.
- Posts link OpenSpec changes and `docs/` reference material — they never
  replace them.