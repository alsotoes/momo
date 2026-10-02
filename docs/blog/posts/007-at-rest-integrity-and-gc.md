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
Storage that quietly returns wrong bytes is worse than storage that fails
loudly. A corrupted read that looks successful can poison downstream systems for
days before anyone notices. This arc turned momo's content-addressing property
into explicit, enforced at-rest integrity.

## The Real Problem

Because momo names every blob by the SHA-256 hash of its contents
([004](004-cas-content-addressable-store.md)), the stored key **is** the
checksum. In principle that makes read-path integrity free: re-hash the bytes,
compare to the key, and you know whether they are intact. But "in principle" was
doing a lot of work. Verification was scattered across handlers, some paths
skipped it, and there was no consistent answer to *what happens* when a mismatch
is found.

We also had a garbage-collection gap. Deleting an object had to remove its bytes
without deleting a blob another object still referenced — and the deletion path
had no single, auditable owner.

## What Landed

{{< diagram src="/diagrams/08-integrity-pipeline.svg" alt="At-rest integrity pipeline" caption="At-rest integrity pipeline" >}}

- **Central integrity verification** (#911): one validate-then-verify path shared
  by every reader, instead of ad-hoc checks spread through handlers. A metadata
  accessor was added so a metadata query no longer has to open the content
  stream just to read a size or a timestamp.
- **Verify-on-read with corruption surfacing** (#925): when a re-hash does not
  match the key, the replica is marked *suspect* rather than silently served.
  That suspicion is the input to the degraded-read and self-heal arc
  ([020](020-r2-degraded-read-self-heal.md)).
- **Checksums on the wire**: S3 clients can ask for a checksum and momo echoes
  it back, so a caller can verify the transfer end to end
  ([012](012-s3-integrity-checksums.md)).
- **Garbage collection**: a *tombstone* — a deletion marker recorded in metadata
  — drives a reference-count sweep. Applying a tombstone deletes the blob
  content, failures to delete are surfaced instead of swallowed, and the sweep
  entry point is guarded against being started twice.

## 🛡 Sentinel lens

At-rest integrity is a security property, not just a performance nicety: silent
bitrot becomes a *loud, auditable* mismatch that the system can act on. The same
sweep that added verification also fixed CRLF injection and path traversal in the
blob and metadata layers ([015](015-sentinel-security-audit.md)). The principle
we traced through it: **integrity checks must be compiled into the core and can
never be skipped by swapping a seam.** If a backend could opt out of
verification, the guarantee would be only as strong as the weakest backend.

## ⚡ Bolt lens

- A single combined metadata read replaced three separate key/value views,
  reducing write-path transactions and CPU.
- Verify-on-read reuses the zero-escape SHA-256 hashing path from
  [024](024-bolt-performance-engineering.md), so verification does not allocate
  per object.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Specs: `openspec/changes/core-integrity-verification`,
  `openspec/changes/storage-at-rest-integrity`.
- Pull requests: #911 (central verification), #925 (verify-on-read).
- Related: [004](004-cas-content-addressable-store.md),
  [012](012-s3-integrity-checksums.md),
  [020](020-r2-degraded-read-self-heal.md),
  [015](015-sentinel-security-audit.md).
