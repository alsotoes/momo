---
title: 'CRUSH-lite: Deterministic Weighted Placement'
date: 2026-06-30 13:09:01+00:00
draft: false
post_type: architecture
tags:
- go
- crush
- placement
- hashing
- bolt
categories:
- storage
summary: Weighted rendezvous hashing picks replicas deterministically and peer-equally
  without a central allocator — later hardened for failure domains.
artifacts:
- type: doc
  path: docs/CRUSH.md
- type: pr
  id: '872'
- type: pr
  id: '873'
- type: spec
  path: openspec/changes/r1-failure-domains
related:
- 004-cas-content-addressable-store
- 019-r1-failure-domain-placement
- 024-bolt-performance-engineering
- 022-momofs-posix-core
---
 CRUSH-lite: Deterministic Weighted Placement

`docs/CRUSH.md` ratifies a **CRUSH-lite** placement scheme: weighted
rendezvous hashing inspired by RADOS CRUSH but kept deliberately small. For a
given object hash, every node independently computes the same ordered replica
list — **no central allocator, no coordination, no RPC to answer "where does
this live?"**.

## The core algorithm

- Score each candidate replica by a hash of (placement-key ⊗ node-id).
- **Weighted** scoring so racks/zones/nodes with more capacity host more
  replicas (nodes with `weight <= 0` were initially a gap — fixed).
- Sort deterministically; stable ordering matters (#872 replaced an unstable
  `sort.Slice` so tied scores can't flip replica order between nodes).
- Fold the hash into a 52-bit mantissa to avoid float64 precision loss when
  converting the score (#873).

## Why content-hash-based placement

Because momo is content-addressed
([004](004-cas-content-addressable-store.md)), the placement key is already a
SHA-256 digest. CRUSH turns that hash into "which 3 nodes own this blob" with
O(1) CPU — a perfect fit for the **Bolt** idea of doing work once, cheaply, and
deterministically.

## From CRUSH-lite to failure domains

CRUSH alone chose replicas; it did **not** constrain *where* they land. R1
([019](019-r1-failure-domain-placement.md)) later layered rack/zone/DC grouping
onto this base so replicas stop landing in the same failure domain. CRUSH is the
foundation; R1 is the awareness.

## ⚡ Bolt lens

Placement is the canonical "no hidden allocation in the hot path" example — the
hash-to-float conversion and stable scoring were micro-corrected in #872/#873
specifically because nondeterminism and float drift are silent correctness bugs
at scale. See [024](024-bolt-performance-engineering.md).

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Upstream: [004](004-cas-content-addressable-store.md). Downstream:
[019](019-r1-failure-domain-placement.md), [024](024-bolt-performance-engineering.md).