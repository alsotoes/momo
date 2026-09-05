---
title: 'At-Rest Integrity: Verify-on-Read, Checksums, and GC'
date: 2026-08-24 19:36:43+00:00
draft: false
post_type: architecture
tags:
- go
- integrity
- checksum
- gc
- sentinel
- bolt
categories:
- storage
summary: 'CAS keys make verify-on-read free: central verification, x-amz-checksum
  echo, tombstone GC, and corruption surfacing.'
artifacts:
- type: pr
  id: '911'
- type: pr
  id: '925'
- type: spec
  path: openspec/changes/core-integrity-verification
- type: spec
  path: openspec/changes/storage-at-rest-integrity
related:
- 004-cas-content-addressable-store
- 012-s3-integrity-checksums
- 015-sentinel-security-audit
- 020-r2-degraded-read-self-heal
- 024-bolt-performance-engineering
- 031-core-integrity-verification
- 043-reduce-read-verify-hashing
---
 At-Rest Integrity: Verify-on-Read, Checksums, and GC

Because momo is content-addressed ([004](004-cas-content-addressable-store.md)),
the stored key **is** the checksum — so read-path integrity is structurally
free. This arc turned that property into explicit machinery.

## What landed

- **Central integrity verification** in the storage layer (#911): one
  validate→verify path shared by all readers, instead of ad-hoc checks spread
  through handlers. `Store.GetMeta` was added so `QueryGet` stops opening the
  content stream just to see metadata.
- **Verify-on-read with corruption surfacing** (#925): on hash mismatch a
  replica is marked suspect — the input to the degraded-read/self-heal arc
  ([020](020-r2-degraded-read-self-heal.md)).
- **Checksums on the wire**: `x-amz-checksum-*` echo for S3 clients
  ([012](012-s3-integrity-checksums.md)).
- **GC**: tombstones drive refcount-sweep; `ApplyTombstone` deletes blob content,
  `ts.Delete` failures are surfaced, and `StartGC` is guarded against double
  invocation.

## 🛡 Sentinel lens

At-rest integrity is a security property: silent bitrot becomes a *loud,
auditable* mismatch. The same sweep that added verification also fixed CRLF
injection and path traversal in the blob/metadata layer
([015](015-sentinel-security-audit.md)). The traced rule: **integrity checks
must be compiled into the core, never skip-able via a seam** (Rule 74).

## ⚡ Bolt lens

- Single combined bbolt metadata read replaced three views (fewer write-path
  transactions, less CPU/latency).
- Verify-on-read reuses the zero-escape SHA-256 path from
  [024](024-bolt-performance-engineering.md).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

[004](004-cas-content-addressable-store.md) · [012](012-s3-integrity-checksums.md)
· [020](020-r2-degraded-read-self-heal.md) · [015](015-sentinel-security-audit.md).