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

momo did not begin its life as an enterprise-grade distributed storage system.
It started as an architectural research project: an experimental Go sandbox built
to explore how distributed storage protocols behave when their internal
replication algorithms are forced to change dynamically under real-time network
pressure.

The name itself — *momo* — hints at this polymorphic origin. As the experimental
codebase grew, though, the hard realities of distributed state machines, network
partitions, and storage durability forced a series of architectural shifts. This
post is the story of that arc.

```
===================================================================================
                              THE EVOLUTIONARY ARC
===================================================================================

Phase 0: File Replication Playground
- In-memory node map, file copies routed across ad-hoc TCP listeners.
- Fragile name-based addressing; race conditions during concurrent overwrites.
        |
        v
Phase 1: The Polymorphic Engine
- Formalized replication topologies: None, Splay, Chain, Primary-Splay.
- Dynamic runtime mode shifting via single-authority controller.
        |
        v
Phase 2: Content-Addressable Storage (CAS)
- Replaced filename addressing with cryptographic SHA-256 digests.
- Embedded Bbolt metadata engine; immutable tiered blob layout.
        |
        v
Phase 3: Production Distributed Storage
- Deterministic CRUSH-lite placement across failure domains.
- S3-compatible gateway with SigV4 and streaming multipart upload.
- POSIX FUSE filesystem interface (momofs) over the CAS core.
```

---

## 1. The Trap of Name-Addressed Storage

In the early prototypes, data was addressed by filename. When a client uploaded
`document.pdf` to one daemon, that daemon wrote a local file named
`document.pdf` and dialed two peer daemons to stream the same bytes to identical
paths.

This simplistic approach collapsed under basic concurrency:

- **Write collisions.** If two clients uploaded different contents under the
  same filename to different cluster nodes, the nodes entered an unresolvable
  split-brain state.
- **Corrupted sync.** Resolving those conflicts required heavyweight two-phase
  locking (2PC) or a distributed lock manager (DLM), dragging request latency
  down by orders of magnitude.
- **Silent corruption.** If a bit flipped on a replica's hard drive, the system
  could not determine which replica held the genuine data without reading all
  copies and taking a majority vote.

The epiphany was to stop naming data by what a user calls it and start naming it
by what it *is*. **Content-addressable storage** (CAS) names every blob after a
cryptographic hash of its bytes — in momo's case, SHA-256. Two identical uploads
produce the same hash, so writes become naturally idempotent and deduplicated.
A user-facing filename becomes transient metadata — a pointer stored in an
embedded key/value database — rather than the storage address itself. That
single decision, covered in [004](004-cas-content-addressable-store.md), unlocked
the rest of the design.

---

## 2. The Invariant Core: Simplicity and Zero Dependencies

As momo grew from a playground into an object store, it resisted the bloat
common to modern distributed filesystems. Three durable rules were codified:

1. **Zero external runtime dependencies.** momo requires no external consensus
   clusters (no ZooKeeper, etcd, or Consul). Everything — from the embedded
   metadata database to SWIM gossip membership — is compiled directly into a
   single static Go binary.
2. **Deterministic placement over central coordination.** Instead of
   maintaining a central lookup master that records where every block lives
   (the way HDFS's NameNode does), momo computes placement locally. Given an
   object hash and a cluster map, every node and client deterministically
   calculates the exact replica set with zero network round-trips. We call this
   **CRUSH-lite**, after Ceph's CRUSH algorithm but with a flat, simpler
   topology ([005](005-crush-placement.md)).
3. **Seams over plugins.** Extensibility points — storage backends, verifiers,
   durability barriers — are designed as compile-time interfaces rather than
   dynamically loaded RPC plugins. A *seam* is a deliberately narrow interface
   we can substitute without paying the IPC and security costs of a plugin
   runtime. The principle is captured in our engineering standards.

---

## 3. Tradeoffs: The Evolution from Sandbox to Engine

| Architectural Aspect | Playground Prototype | Distributed Object Store (momo) |
|---|---|---|
| **Addressing Model** | User-defined paths / filenames | SHA-256 content-addressed storage |
| **Storage Backend** | Flat temp-directory files | Tiered fan-out (`blobs/ab/cd/ef/...`) + embedded index |
| **Placement Logic** | Hardcoded node lists | Deterministic CRUSH-lite with failure-domain awareness |
| **Durability Semantics** | Unchecked OS buffer flush | Strict flush-before-ack, group commit, survivor quorum |
| **Interface Surface** | Custom CLI commands only | Full AWS S3 HTTP API + POSIX FUSE mount (`momofs`) |

Each shift traded simplicity for durability, and each one only happened because
the previous prototype failed under a real workload. momo proved that a storage
system can provide production-level durability and S3 compatibility while
preserving the agile, hackable essence of a minimalist Go playground — provided
you are willing to give up centralized coordination.

## References / Dig deeper

- Origin commit: `a8114af4`.
- Sibling posts: [002: Replication Strategies](002-replication-strategies-polymorphic.md),
  [004: Content-Addressable Storage](004-cas-content-addressable-store.md),
  [005: CRUSH Placement](005-crush-placement.md),
  [028: Roadmap and Research](028-roadmap-and-research.md).
- The "seam over plugins" mindset and the project's engineering standards:
  [docs/STANDARDS.md](../../STANDARDS.md).
