---
title: "Origin: From Replication Playground to Object Store"
date: 2025-09-09T20:10:05Z
draft: false
post_type: architecture
tags: [go, origin, architecture]
categories: [origin]
summary: "How momo started as a file-replication playground in Go and grew into a distributed content-addressed object store."
artifacts:
  - {type: commit, id: "a8114af4"}
related:
  - 002-replication-strategies-polymorphic
  - 004-cas-content-addressable-store
---

momo did not begin its life as an enterprise-grade distributed storage system. It started as an architectural research project: an experimental Go sandbox designed to explore how distributed storage protocols behave when their internal replication algorithms are forced to mutate dynamically under real-time network pressure.

The name itself—*momo*—hints at this polymorphic origin. But as the experimental codebase grew, the realities of distributed state machines, network partitions, and storage durability forced a series of profound architectural shifts.

```
===================================================================================
                              THE EVOLUTIONARY ARC
===================================================================================

Phase 0: File Replication Playground
- In-memory node map, file copies routed across ad-hoc TCP listeners.
- Fragile name-based addressing; race conditions during concurrent overwrites.
        |
        v
Phase 1: The Polymorphic Engine (PR #2)
- Formalized replication topologies: None, Splay, Chain, Primary-Splay.
- Dynamic runtime mode shifting via single-authority controller.
        |
        v
Phase 2: Content-Addressable Storage (CAS)
- Replaced filename addressing with cryptographic SHA-256 digests.
- Embedded Bbolt metadata engine; immutable tiered blob layout.
        |
        v
Phase 3: Production Distributed Storage (R1–R6)
- Deterministic CRUSH-lite placement across failure domains.
- AWS S3 compatible gateway with SigV4 and streaming multipart upload.
- POSIX FUSE filesystem interface (momofs) over the CAS core.
```

---

## 1. The Trap of Name-Addressed Storage

In early prototypes, when client node $C$ uploaded `document.pdf` to daemon $D_1$, $D_1$ created a local file named `document.pdf` and dialed peer daemons $D_2$ and $D_3$ to stream the same bytes to identical paths.

This simplistic approach collapsed under basic concurrency:
- **Write Collisions**: If two clients concurrently uploaded different contents with the same filename to different cluster nodes, the nodes entered an unresolvable split-brain state.
- **Corrupted Sync**: Resolving conflicts required heavy two-phase locking (2PC) or distributed lock managers (DLM), dragging request latency down by orders of magnitude.
- **Silent Corruption**: If a bit flipped on a replica's hard drive, the system could not determine which replica held the genuine data without reading all copies and taking a majority vote.

The epiphany was adopting **Content-Addressable Storage (CAS)** ([004](004-cas-content-addressable-store.md)). By naming blobs after their SHA-256 hashes, writes became naturally idempotent and deduplicated. Filenames became transient metadata pointers stored in an embedded Bbolt database.

---

## 2. The Invariant Core: Simplicity and Zero Dependencies

As momo grew from a playground into an object store, it resisted the bloat common to modern distributed filesystems. Three durable rules were codified:

1. **Zero External Runtime Dependencies**: momo requires no external consensus clusters (no ZooKeeper, etcd, or Consul). Everything—from Bbolt metadata to SWIM gossip membership—is compiled directly into a single static Go binary.
2. **Deterministic Placement over Central Coordination**: Instead of maintaining a central lookup master that records where every block lives (like HDFS NameNode), momo adopted **CRUSH-lite** ([005](005-crush-placement.md)). Given an object hash and cluster map, every node and client deterministically calculates the exact replica set with zero network round-trips.
3. **Seams Over Plugins (Rule 74)**: Extensibility points (storage backends, verifiers, durability barriers) are designed as compile-time interfaces, avoiding the IPC and security penalties of dynamic RPC plugins.

---

## 3. Tradeoffs: The Evolution from Sandbox to Engine

| Architectural Aspect | Playground Prototype | Distributed Object Store (momo) |
|---|---|---|
| **Addressing Model** | User-defined paths / filenames | SHA-256 Content-Addressed Storage (CAS) |
| **Storage Backend** | Flat `/tmp` directory files | Tiered fan-out (`blobs/ab/cd/ef/...`) + Bbolt index |
| **Placement Logic** | Hardcoded node lists | Deterministic CRUSH-lite with domain awareness |
| **Durability Semantics** | Unchecked OS buffer flush | Strict `fsync`-before-ack, group commit, survivor quorum |
| **Interface Surface** | Custom CLI commands only | Full AWS S3 HTTP API + POSIX FUSE mount (`momofs`) |

momo proved that a storage system can provide production-level durability and enterprise S3 compatibility while preserving the agile, hackable essence of a minimalist Go playground.

## Related

- Polymorphic replication engine: [002](002-replication-strategies-polymorphic.md)
- Content-addressed store: [004](004-cas-content-addressable-store.md)
- Deterministic placement: [005](005-crush-placement.md)
- Production readiness roadmap: [028](028-roadmap-and-research.md)
