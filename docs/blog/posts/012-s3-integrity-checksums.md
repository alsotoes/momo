---
title: 'S3 Integrity Checksums: x-amz-checksum-*'
date: 2026-08-24 16:36:34+00:00
draft: false
post_type: architecture
tags:
- go
- s3
- checksum
- integrity
- sentinel
categories:
- s3
summary: x-amz-checksum-CRC32/SHA256 echo on PUT; integrity surfaced to clients so
  dirty bytes never masquerade as clean.
artifacts:
- type: pr
  id: '902'
- type: spec
  path: openspec/changes/s3-integrity-checksums
related:
- 007-at-rest-integrity-and-gc
- 008-s3-gateway-core
- 009-s3-multipart-and-breadth
- 031-core-integrity-verification
- 045-bolt-lastmodified-header
---
 S3 Integrity Checksums: x-amz-checksum-*

Part 2 of #820's integrity push: the S3 gateway echoes `x-amz-checksum-CRC32`
(and friends, incl. SHA256) on request/response so a client can confirm the
object it wrote is byte-for-byte the object it reads — **without trusting the
server**.

## Why S3-level checksums matter

S3 SDKs/tooling use these headers for end-to-end integrity:
- **PUT/POST**: client supplies or requests the checksum; server verifies and
  stores it.
- **GET**: server returns the stored checksum; client compares in the existing
  response path.

For a content-addressed store ([004](004-cas-content-addressable-store.md)) the
checksum is *already computed* on ingest. Surfacing it to clients costs nothing
extra and converts CAS's internal property into a **client-verifiable contract**
— the honest, fail-loud posture from
[011](011-s3-https-tls-enforcement.md), applied to bytes.

## Sentinel + Bolt overlap

- **Sentinel**: a checksum-echo API is only as good as its honesty — the
  central verify-on-read machinery ([007](007-at-rest-integrity-and-gc.md))
  guarantees the echoed value reflects the bytes actually stored.
- **Bolt**: computing/re-fetching checksums on every GET must not add
  allocations to the read path; server-side hashes are cached with metadata
  (see [024](024-bolt-performance-engineering.md)).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Integrity core: [007](007-at-rest-integrity-and-gc.md). Gateway:
[008](008-s3-gateway-core.md). Performance: [024](024-bolt-performance-engineering.md).