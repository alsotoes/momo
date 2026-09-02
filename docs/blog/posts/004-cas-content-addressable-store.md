---
title: 'CAS: Content-Addressable Storage and Deduplication'
date: 2026-03-11 14:34:18+00:00
draft: false
tags:
- go
- cas
- storage
- sha256
- bolt
categories:
- storage
summary: Identify every object by its SHA-256 content hash — server-side dedup, verify-on-read,
  and a path that later mounted a filesystem.
artifacts:
- type: spec
  path: openspec/changes/add-cas-storage
- type: pr
  id: '838'
- type: issue
  id: '820'
related:
- 001-origin-and-genesis
- 005-crush-placement
- 006-pluggable-storage-backends
- 007-at-rest-integrity-and-gc
- 022-momofs-posix-core
- 029-fuse-go-fuse-v2-migration
- 016-p2p-gossip-swim
- 023-momofs-fuse-transport
- 031-core-integrity-verification
- 037-zero-crash-hardening-patterns
---
CAS: Content-Addressable Storage and Deduplication

The pivotal decision: **store objects by their SHA-256 content hash, not by
name.** Names became metadata pointing at a blob address. Two objects with
identical bytes collapse to one stored blob — dedup by construction,
within a node.

## Why content addressing

1. **Deduplication** — the same file uploaded twice costs one blob (server-side,
   per-node).
2. **Integrity for free** — the key *is* the checksum; verify-on-read can hash
   and compare without a separate index.
3. **A single write chokepoint** — validate → write. Everything (native PUT, S3
   gateway, replication, FUSE mount) funnels through it. This became a *core
   trust invariant* per Rule 74.

## The storage path genealogy

- The blob layer became a pluggable **backend** (`local`, `nfs`, `s3`, `raw`)
  — see [006](006-pluggable-storage-backends.md).
- Placement per object went to **CRUSH-lite** — see [005](005-crush-placement.md).
- Verification grew into **at-rest integrity + GC** — see
  [007](007-at-rest-integrity-and-gc.md).
- Metadata held in **bbolt** (shared vs local buckets) with blob data a
  hash-addressed file — see `docs/momofs/CURRENT_ARCHITECTURE.md` (TODO: mark
  implemented components only).

## ⚡ Bolt + 🛡 Sentinel lens

- **Bolt**: zero-escape SHA-256 hashing and hex encoding use stack-allocated
  buffers; hashing hot paths are profiled in the perf arc
  ([024](024-bolt-performance-engineering.md)).
- **Sentinel**: `GetBlobPath` on non-local backends, legacy-metadata parse
  failures, and raw-store path traversal were singled out in the security audit
  (see [015](015-sentinel-security-audit.md)). CRLF injection in metadata and
  request-smuggling surfaced in the same sweep.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Parent: [001](001-origin-and-genesis.md). Edges: [005](005-crush-placement.md),
[006](006-pluggable-storage-backends.md), [007](007-at-rest-integrity-and-gc.md),
[022](022-momofs-posix-core.md).