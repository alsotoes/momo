# MomoFS — Distributed Object Storage Cluster Filesystem

MomoFS is a **distributed masterless ring architecture** supporting multi-region replication and fault tolerance. No external database cluster is required — the Momo nodes themselves form the ring.

This directory contains the complete MomoFS design documentation, split into focused files.

## Document Index

| Document | Description | Lines |
|----------|-------------|-------|
| [CURRENT_ARCHITECTURE.md](CURRENT_ARCHITECTURE.md) | Current BoltDB schema, buckets, what's shared vs. local | ~70 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Masterless ring topology, data flow, consistency model, multi-region, storage engine comparison | ~290 |
| [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) | 6 formal design decisions (DD-1 to DD-6) + Ceph comparison | ~180 |
| [DESIGN_PRINCIPLES.md](DESIGN_PRINCIPLES.md) | 14 design principles, Read From Any, HPC Ready, Cloud Ready | ~160 |
| [IMPLEMENTATION.md](IMPLEMENTATION.md) | Four Pillars implementation: Go interfaces, protocols, 100-node walkthrough, config, Phase 1 tasks | ~1016 |
| [LIMITATIONS.md](LIMITATIONS.md) | Current gaps and architecture transition (local → distributed) | ~80 |
| [SCRUB_HEALING.md](SCRUB_HEALING.md) | Shallow/deep scrub, repair queue, self-healing design | ~60 |
| [MULTI_TENANCY.md](MULTI_TENANCY.md) | Tenant model, per-tenant auth/quotas/encryption, BoltDB schema changes | ~60 |
| [GDPR.md](GDPR.md) | Right to erasure, portability, data residency, encryption at rest | ~30 |
| [AI_SEARCH.md](AI_SEARCH.md) | Vector embeddings, content classification, semantic search, multi-modal search | ~80 |
| [RECOVERY.md](RECOVERY.md) | WAL journaling, Merkle trees, erasure coding, directory operations | ~80 |
| [ADAPTIVE_SYSTEMS.md](ADAPTIVE_SYSTEMS.md) | Biological inspiration: stigmergy, immune system, neuroplasticity, etc. | ~400 |
| [PERFORMANCE_SECURITY.md](PERFORMANCE_SECURITY.md) | ⚡ Bolt & 🛡️ Sentinel applied to every MomoFS feature | ~400 |
| [COMPARISON.md](COMPARISON.md) | Feature comparison: MomoFS vs Ceph, Lustre, ScyllaDB, IPFS | ~250 |
| [LESSONS_LEARNED.md](LESSONS_LEARNED.md) | Actionable features to adopt from Ceph, Lustre, ScyllaDB, IPFS | ~280 |
| [ROADMAP.md](ROADMAP.md) | 8-phase roadmap, BoltDB evolution summary, SPOF checklist | ~120 |

## Quick Links

**Start here**: [ARCHITECTURE.md](ARCHITECTURE.md) — the masterless ring overview.

**How reads work**: [IMPLEMENTATION.md](IMPLEMENTATION.md) section "1.1a Concrete Walkthrough" — traces a 100-node cluster write+read step by step.

**Why no external DB**: [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) DD-1.

**Comparison with Ceph**: [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) section "Comparison with Ceph".

**What needs to be built**: [ROADMAP.md](ROADMAP.md) — 8-phase implementation roadmap.

## Related Documents

- [../ARCHITECTURE.md](../ARCHITECTURE.md) — Current Momo system architecture
- [../REPLICATION_STRATEGIES.md](../REPLICATION_STRATEGIES.md) — Chain and Splay replication
- [../P2P.md](../P2P.md) — Gossip membership, SWIM, lease consensus
- [../CRUSH.md](../CRUSH.md) — Data placement algorithm
- [../ROADMAP.md](../ROADMAP.md) — Existing project roadmap
