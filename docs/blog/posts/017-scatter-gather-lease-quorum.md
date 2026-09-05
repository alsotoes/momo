---
title: 'Scatter-Gather and Lease Consensus: Quorum Math'
date: 2026-08-13 07:58:21+00:00
draft: false
post_type: architecture
tags:
- go
- p2p
- lease
- quorum
- governance
categories:
- p2p
summary: Scatter-gather lists across shard owners; leases with correct majority quorum
  — and the two nasty off-by-one/partition bugs found by audit.
artifacts:
- type: pr
  id: '806'
- type: pr
  id: '810'
- type: spec
  path: openspec/changes/gossip-scatter-lease
related:
- 016-p2p-gossip-swim
- 021-r3-write-durability-quorum
- 015-sentinel-security-audit
- 014-confidential-dedup-oprf
- 018-adaptive-scaling-peer-quality
- 020-r2-degraded-read-self-heal
- 032-r5-metrics-phases-2-4
---
catter-Gather and Lease Consensus: Quorum Math

Metadata doesn't live in a database — it lives spread across the ring's shard
owners. `GlobalList` and metadata writes use **scatter-gather**, and mutable
leases use a **majority quorum**. Both required the audit treatment.

## Scatter-gather listing

`ListObjectsV2`/`GlobalList` fans out to shard owners, merges metadata lists,
dedups by content hash (dropping alternate names), paginates. The momofs
design later narrowed the fan-out to *prefix-relevant shard owners* only
(`docs/momofs/IMPLEMENTATION.md §2.3`, implemented in `src/momofs`).

## Leases and the majority-quorum bug class

Leases protect mutable shared state (rename, refcount). Two audit-caught bugs:

- **#806** — the majority formula was **off by one** for odd peer counts: for
  3 peers, `2` was required but code asked for a plain `> half` that sometimes
  resolved too low. Correct: `votes > peers/2` computed on the right integer
  basis.
- **#810** — lease acquisition **succeeding with zero quorum during a network
  partition** — the split-brain enabler. Now: fail closed; no quorum, no lease.

## ⚡ Bolt lens

Scatter-gather merge + lease voting mutate shared state; both honor the
zero-allocation merge discipline (merge by hash, stable-order) to keep list hot
paths allocation-free. `EncodeFileMetadataList` gained length limits so a
malicious oversized entry can't blow the message buffer (#851).

## Sentinel + governance note

Lease quorum is a *correctness* bug — but the fix (#810) is security-adjacent:
a partition granting a lease is how a split ring corrupts refcounts. This is
exactly the "fail-loud" Sentinel posture from
[015](015-sentinel-security-audit.md).

## Related

Membership: [016](016-p2p-gossip-swim.md). Durability: [021](021-r3-write-durability-quorum.md).
Audit: [015](015-sentinel-security-audit.md).