---
title: 'S3 Gateway Core: XML, Buckets, Objects, Lists'
date: 2026-08-11 18:25:16+00:00
draft: false
tags:
- go
- s3
- gateway
- xml
- sentinel
categories:
- s3
summary: 'The S3 gateway: SigV4, S3-compliant XML errors, Head/List/Copy, pagination,
  ranges, and metadata — a compatible API surface for real clients.'
artifacts:
- type: pr
  id: '782'
- type: pr
  id: '783'
- type: pr
  id: '785'
- type: spec
  path: openspec/changes/add-s3-protocol
related:
- 006-pluggable-storage-backends
- 009-s3-multipart-and-breadth
- 010-s3-auth-presigned-sigv4
- 012-s3-integrity-checksums
- 011-s3-https-tls-enforcement
---
 S3 Gateway Core: XML, Buckets, Objects, Lists

Momo speaks S3 — that is the compatibility bet that made it usable by real
tooling (aws-cli, SDKs, rclone). The gateway is a thin, stateless adapter over
the native store: **S3 request → Store call**, no separate S3 data model.

## The incoming wave (Aug 11, 2026)

- **S3-compliant XML error responses** (#782) — not Go `text/plain`; real error
  XML with codes, matching what SDKs parse.
- **HeadObject / HeadBucket** (#783) — metadata without the body.
- **Bucket management** (#784) — CreateBucket / DeleteBucket / ListBuckets.
- **ListObjectsV2 pagination** (#785) — continuation-token — with a subtle fix
  later: `KeyCount` must exclude `CommonPrefixes`.
- **Range + conditional headers** (#786), **object metadata**
  (`Content-Type`, `x-amz-meta-*`) (#787), **CopyObject + batch DeleteObjects**
  (#788).

## Architecture: an adapter, not a server

The S3 gateway holds **no state of its own** — it maps HTTP verbs onto the
pluggable Store seam ([006](006-pluggable-storage-backends.md)). That's why the
same bytes appear via native PUT, replication, and the mount later — one CAS,
many doors ([004](004-cas-content-addressable-store.md)).

## 🛡 Sentinel lens

S3 is the widest untrusted surface. The same month produced the security sweep
([015](015-sentinel-security-audit.md)): HTTP request smuggling, header-read
limit leaks, missing body-close, and CRLF injection were all found in/around the
gateway and fixed as part of the hardening arc. HTTPS/TLS enforcement followed
in [011](011-s3-https-tls-enforcement.md).

## ⚡ Bolt lens

List handlers were the most allocation-heavy code in the gateway — a series of
Bolt optimizations attacked exactly there (see
[024](024-bolt-performance-engineering.md)).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

[006](006-pluggable-storage-backends.md) · [009](009-s3-multipart-and-breadth.md)
· [010](010-s3-auth-presigned-sigv4.md) · [011](011-s3-https-tls-enforcement.md)
· [012](012-s3-integrity-checksums.md).