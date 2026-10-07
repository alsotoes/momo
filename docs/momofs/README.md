# MomoFS — Distributed Object Storage Cluster Filesystem

MomoFS is a **distributed masterless ring architecture** supporting multi-region replication and fault tolerance. No external database cluster is required — the Momo nodes themselves form the ring.

The momofs core (CAS inode/metadata layer) and FUSE transport (`momo -imp fs`) are **implemented** (openspec/changes/r4-momofs/, #932 → #963); see [MOUNT_USER_GUIDE.md](MOUNT_USER_GUIDE.md). The remaining design docs below cover the full roadmap and future phases.

## Elevated Foundational Principles

The core architectural principles originally drafted here have been elevated to canonical top-level documentation:
- [**Core Design Principles**](../DESIGN_PRINCIPLES.md) — 14 foundational tenets (Read From Any Node, Zero SPOF, HPC/Cloud Ready)
- [**Adaptive Systems Architecture**](../ADAPTIVE_SYSTEMS.md) — 16 biological models (Ant Colony, Epigenetics, Homeostasis, Stigmergy)
- [**Architectural Decisions (DD-1 to DD-6)**](../DESIGN_DECISIONS.md) — Fundamental decisions on BoltDB, dual rings, wrapper interfaces
- [**Distributed Storage Comparison**](../COMPARISON.md) — Feature matrix vs Ceph, Lustre, ScyllaDB, IPFS
- [**Lessons Learned**](../LESSONS_LEARNED.md) — Cross-system patterns and priorities

## MomoFS Subsystem Documents

| Document | Description |
|----------|-------------|
| [MOUNT_USER_GUIDE.md](MOUNT_USER_GUIDE.md) | FUSE mount (`momo -imp fs`) operation, flags, consistency, limitations |
| [CURRENT_ARCHITECTURE.md](CURRENT_ARCHITECTURE.md) | Current BoltDB schema, buckets, what's shared vs. local |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Masterless ring topology, data flow, consistency model, multi-region |
| [IMPLEMENTATION.md](IMPLEMENTATION.md) | Four Pillars implementation: Go interfaces, protocols, 100-node walkthrough |
| [LIMITATIONS.md](LIMITATIONS.md) | Current gaps and architecture transition (local → distributed) |
| [SCRUB_HEALING.md](SCRUB_HEALING.md) | Shallow/deep scrub, repair queue, self-healing design |
| [MULTI_TENANCY.md](MULTI_TENANCY.md) | Tenant model, per-tenant auth/quotas/encryption, BoltDB schema |
| [GDPR.md](GDPR.md) | Right to erasure, portability, data residency, encryption at rest |
| [AI_SEARCH.md](AI_SEARCH.md) | Vector embeddings, content classification, semantic search |
| [RECOVERY.md](RECOVERY.md) | WAL journaling, Merkle trees, erasure coding, directory operations |
| [PERFORMANCE_SECURITY.md](PERFORMANCE_SECURITY.md) | ⚡ Bolt & 🛡️ Sentinel applied across filesystem operations |
| [ROADMAP.md](ROADMAP.md) | 8-phase roadmap, BoltDB evolution summary, SPOF checklist |

## Quick Links

**Start here**: [ARCHITECTURE.md](ARCHITECTURE.md) — the masterless ring overview.

**How reads work**: [IMPLEMENTATION.md](IMPLEMENTATION.md) section "1.1a Concrete Walkthrough" — traces a 100-node cluster write+read step by step.

**Why no external DB**: [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) DD-1.

**Comparison with Ceph**: [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) section "Comparison with Ceph".

**How to mount**: [MOUNT_USER_GUIDE.md](MOUNT_USER_GUIDE.md) — `momo -imp fs` operation, flags, consistency, limitations.

**What happens next**: [ROADMAP.md](ROADMAP.md) — remaining 8-phase implementation roadmap.

## Related Documents

- [../ARCHITECTURE.md](../ARCHITECTURE.md) — Current Momo system architecture
- [../REPLICATION_STRATEGIES.md](../REPLICATION_STRATEGIES.md) — Chain and Splay replication
- [../P2P.md](../P2P.md) — Gossip membership, SWIM, lease consensus
- [../CRUSH.md](../CRUSH.md) — Data placement algorithm
- [../ROADMAP.md](../ROADMAP.md) — Existing project roadmap
