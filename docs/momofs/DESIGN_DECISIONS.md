# Design Decisions
# Architecture Decision Records

All architectural decisions are now documented as **Architecture Decision Records (ADRs)** in `docs/adr/`. Each ADR corresponds to an OpenSpec change under `openspec/changes/`. See `docs/adr/README.md` for the index and process.

---


This section records the key architectural decisions, their alternatives, and the rationale for each choice. These decisions are foundational — changing any of them would require a major redesign.

---

**DD-1: Embedded BoltDB vs External Database Cluster**

| | |
|---|---|
| **Decision** | Use embedded BoltDB on each node. No external database (no Cassandra, ScyllaDB, DynamoDB, etcd, or Redis). |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | MomoFS needs distributed metadata to enable "Read From Any Node." The question is whether to store metadata in an external distributed DB cluster or keep it embedded in each Momo node. |
| **Alternatives** | **A) ScyllaDB** — C++ drop-in Cassandra replacement, millions of ops/sec, single-digit ms latency, petabyte scale. Would handle sharding, replication, failover, multi-region out of the box. **B) etcd/Consul** — Raft-based KV stores, simpler than ScyllaDB but still external. Good for small metadata sets. **C) Embedded BoltDB + our own ring** — Each node runs BoltDB locally, we build consistent hash ring + metadata replication ourselves. |
| **Rationale** | 1. **Latency**: BoltDB is in-process (<0.1ms). ScyllaDB adds a network hop (1-5ms). For a system targeting sub-ms reads, 10-50x difference is unacceptable. 2. **Philosophy**: Momo's core principle is "single binary, zero external dependencies." Adding a DB cluster fundamentally changes the deployment model from `./momo` to `./momo + scylla cluster + monitoring + backups + tuning`. 3. **Scale mismatch**: Momo's metadata is ~1GB for a 1PB cluster (10M objects × ~100 bytes). Sharded across 100 nodes = 10MB/node. ScyllaDB's petabyte-scale wide-column power is overkill for 10MB of KV data. 4. **Data model**: Momo needs KV lookups (`name→hash`, `hash→ObjectMeta`), not wide-column queries with secondary indexes and CQL. BoltDB does exactly this. 5. **Ops burden**: ScyllaDB requires compaction tuning, GC/heap management, repair jobs, schema migrations. BoltDB is self-managing (single B+tree, mmap, no compaction). 6. **Precedent**: Ceph uses the same approach — embedded RocksDB (BlueStore) on each storage node, distributed via RADOS. No external DB. This is a proven architecture at exabyte scale. 7. **Existing infrastructure**: We already have BoltDB working (`src/storage/storage.go`), P2P gossip (`src/p2p/gossip.go`), SWIM failure detection (`src/p2p/peer_map.go`), lease consensus (`src/p2p/lease.go`), and CRUSH placement (`src/common/crush.go`). Building the ring on top of these is less work than integrating and operating an external DB. |
| **Trade-offs accepted** | We must build metadata replication, failover, and conflict resolution ourselves (Phase 1). We don't get CQL, secondary indexes, or materialized views for free. BoltDB has a practical per-node limit of ~100GB (not an issue — metadata is sharded). |
| **Revisit if** | Metadata per node exceeds 50GB (would need ~5PB cluster with 500M+ objects per node), or we need complex cross-shard queries that BoltDB can't support. |

---

**DD-2: Two Independent Rings (Metadata + Data)**

| | |
|---|---|
| **Decision** | Use a consistent hash ring for metadata sharding and CRUSH (weighted rendezvous hashing) for data placement. These are separate rings with independent replication factors. |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | Both metadata and data need to be partitioned across the cluster. Should they use the same algorithm or different ones? |
| **Alternatives** | **A) Single ring for both** — Use CRUSH for both metadata and data. Simpler, one algorithm. **B) Single ring, same RF** — Use CRUSH for both with the same replication factor. Even simpler. **C) Two independent rings** — Consistent hash ring for metadata, CRUSH for data, independent replication factors. |
| **Rationale** | Metadata and data have different optimization goals. **Metadata** needs minimal key remapping on cluster changes (consistent hashing: K/N keys move) and fast lookups (ring lookup is O(1)). **Data** needs rack/zone awareness, weighted placement for heterogeneous nodes, and bandwidth optimization (CRUSH provides all of these). Using the same RF for both is wrong: metadata is small and critical (higher RF = more safety), data is large and expensive (lower RF = less storage overhead). With separate rings, we can have `metadata_replication=5` and `data_replication=3` — more metadata copies for faster reads, fewer data copies to save storage. |
| **Trade-offs accepted** | Two algorithms to maintain. Two config parameters. Slightly more complex cluster join/leave (both rings must update). |
| **Revisit if** | We find that CRUSH's remapping behavior is sufficient for metadata, or the two-ring complexity causes bugs. |

---

**DD-3: DistributedStore Implements Existing Store Interface**

| | |
|---|---|
| **Decision** | `DistributedStore` implements the existing `Store` interface (`Put/Get/Has/Delete/List`). The server daemon (`server.Daemon`) is unchanged. |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | We need to add distributed metadata resolution without rewriting the server. How should the new code integrate? |
| **Alternatives** | **A) New interface** — Define a new `DistributedStore` interface, change `server.Daemon` to use it. **B) Wrapper** — `DistributedStore` wraps `CASStore` and implements the same `Store` interface. Server is unaware. **C) Server-side changes** — Add metadata resolution logic directly in `server.Daemon`. |
| **Rationale** | Option B (wrapper) is the least invasive. The server daemon already calls `store.Get()` — it doesn't care whether the store is local or distributed. All complexity (metadata resolution, blob proxy, caching) is encapsulated in `DistributedStore`. This means: zero changes to `server.go`, `client.go`, `transport/*`, or any existing test. Backward compatibility is trivially preserved (`momofs.enabled = false` → return `CASStore` as before). Testing is easier — `DistributedStore` can be tested independently of the server. |
| **Trade-offs accepted** | The `Store` interface doesn't expose batch operations or streaming metadata. If we need those later, we may need to extend the interface. |
| **Revisit if** | The `Store` interface becomes a bottleneck for distributed operations (e.g., batch metadata resolution for List). |

---

**DD-4: Quorum Writes, Any-Replica Reads**

| | |
|---|---|
| **Decision** | Metadata writes require quorum (`metadata_quorum` of `metadata_replication`, default 2 of 3). Metadata reads accept any replica (ONE). Read repair fixes stale replicas. |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | What consistency level should MomoFS use for metadata operations? |
| **Alternatives** | **A) Strong consistency (QUORUM reads + QUORUM writes)** — Always reads from quorum, slower but always fresh. **B) Eventual consistency (ONE read + ONE write)** — Fastest, but reads may return stale data. **C) QUORUM writes, ONE reads** — Fast reads, writes are durable, read repair fixes staleness. **D) Linearizable (Paxos/Raft per key)** — Strongest, but 2-3x slower writes. |
| **Rationale** | Option C is the best trade-off. QUORUM writes (2 of 3) prevent split-brain — two concurrent writes to the same key can't both succeed on disjoint quorums. ONE reads are fast (single RPC, <1ms). Read repair (return fresh data, background-replicate to stale replica) ensures convergence. This is the same model Cassandra uses by default (QUORUM writes, ONE reads with read repair). For MomoFS, stale reads are tolerable for `List` (eventually consistent listing) but not for `Get` (must return correct data). `Get` can be upgraded to QUORUM reads if needed, at the cost of 2x latency. Vector clocks handle concurrent write conflicts (last-writer-wins by timestamp, logged for review). |
| **Trade-offs accepted** | Brief windows of stale reads (milliseconds to seconds until read repair). Concurrent writes may conflict (resolved by LWW, logged). |
| **Revisit if** | Stale reads cause customer-visible problems. Can upgrade to QUORUM reads per-operation without changing the architecture. |

---

**DD-5: Async Cross-Region Replication**

| | |
|---|---|
| **Decision** | Cross-region replication is asynchronous. Each region has its own independent ring. Writes commit locally, then replicate cross-region in the background. |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | Should cross-region replication be synchronous (cross-region quorum) or asynchronous (local commit, background replicate)? |
| **Alternatives** | **A) Sync (cross-region quorum)** — Writes require ack from both regions. Strong consistency across regions. **B) Async (local commit, background replicate)** — Writes commit locally, replicate cross-region asynchronously. Eventually consistent across regions. **C) Sync for critical tenants, async for others** — Per-tenant consistency level. |
| **Rationale** | Option B (async) because: 1. **Latency**: Cross-region RTT is 50-200ms. Sync writes would add this to every write latency. 2. **Availability**: If the cross-region link goes down, sync writes block. Async writes continue locally — each region maintains full availability during network partition. 3. **Cost**: Sync writes require cross-region bandwidth for every write. Async can batch and compress. 4. **GDPR**: Data residency means some tenants should NOT be replicated cross-region at all. Async per-tenant replication is more flexible. 5. **RPO is configurable**: For tenants needing near-zero RPO, the replication pipeline can be tuned for sub-second lag. 6. **Precedent**: Cassandra, DynamoDB, and Ceph all use async cross-region replication by default. |
| **Trade-offs accepted** | Cross-region data lag (RPO > 0, configurable per tenant). Failover to other region may lose recent writes (last RPO window). Conflict resolution needed (vector clocks + LWW). |
| **Revisit if** | A tenant requires zero RPO (sync cross-region). Can add per-tenant sync mode without changing the architecture. |

---

**DD-6: Separate Metadata Replication Factor from Data Replication Factor**

| | |
|---|---|
| **Decision** | `metadata_replication` (default 3) is independent from `replication_factor` (default 3, existing). They can be configured separately. |
| **Status** | Accepted |
| **Date** | 2026-07-29 |
| **Context** | Should metadata and data have the same replication factor, or separate ones? |
| **Alternatives** | **A) Same RF for both** — Simpler config, one number. **B) Separate RFs** — `metadata_replication` and `replication_factor` are independent. |
| **Rationale** | Metadata and data have different value-density and access patterns. Metadata is tiny (~100 bytes per object) but accessed on every read/write — losing it means the object is unreachable. Data is large (MB-GB) but only accessed when the blob is read — losing one replica is tolerable (other replicas exist). With separate RFs, you can set `metadata_replication=5` for high metadata durability without paying for 5x data storage. Conversely, for a cost-sensitive deployment, you can set `data_replication=2` but `metadata_replication=3` — fewer data copies (save storage) but more metadata copies (fast reads). |
| **Trade-offs accepted** | Two config parameters instead of one. Users must understand the difference. |
| **Revisit if** | The config complexity confuses users. Can default both to 3 and let advanced users override. |

---

#### 2.0.10 Comparison with Ceph

Ceph is the most successful open-source distributed storage system, with exabyte-scale production deployments. MomoFS shares Ceph's core architecture but is simpler, more modern, and adds capabilities Ceph doesn't have. This section maps the two systems side by side.

**Architectural comparison:**

| Aspect | Ceph | MomoFS | Advantage |
|--------|------|--------|-----------|
| **Placement** | CRUSH (weighted rendezvous hashing) | CRUSH for data + consistent hash ring for metadata | MomoFS: metadata ring has minimal remapping on cluster changes |
| **Membership** | MON daemons (3-5 dedicated nodes, Paxos) | P2P gossip + SWIM (all nodes, no dedicated monitors) | MomoFS: no separate monitor daemons, simpler deployment |
| **Metadata** | MDS daemons (separate metadata servers for CephFS) | Embedded BoltDB on every node (no separate metadata servers) | MomoFS: no MDS bottleneck, metadata distributed across all nodes |
| **Local storage engine** | BlueStore (RocksDB, C++) | BoltDB (pure Go) | MomoFS: zero dependencies, simpler. Ceph: more mature, SPDK support |
| **Object storage** | RADOS (librados) + RGW (S3/Swift gateway) | S3 API (built into every node) | MomoFS: any node is an S3 endpoint, no separate gateway |
| **Filesystem** | CephFS (POSIX, FUSE or kernel) | FUSE mount (via S3 API) | Ceph: kernel client, deeper POSIX. MomoFS: simpler, client-side only |
| **Block device** | RBD (RADOS Block Device for VMs, QEMU, OpenStack) | Not planned | Ceph: mature RBD. MomoFS: not a target use case |
| **Replication** | Replicated PG (placement groups) + erasure coding | Chain/Splay/Primary-Splay + erasure coding (Phase 7) | Ceph: mature. MomoFS: polymorphic (runtime strategy switching) |
| **Self-healing** | PG scrub, deep scrub, recovery | Shallow scrub, deep scrub, repair (Phase 2) | Ceph: mature. MomoFS: same design, not yet implemented |
| **Multi-tenancy** | CephX auth, per-pool quotas, tenant isolation | Per-tenant auth, quotas, encryption (Phase 3) | Comparable design |
| **Erasure coding** | Mature (Reed-Solomon, plugin API) | Planned Phase 7 | Ceph: mature. MomoFS: planned |
| **Transport** | TCP (messenger v2) | TCP + QUIC + S3 REST | MomoFS: QUIC (encrypted, multiplexed), chameleon routing |
| **Deployment** | Multiple daemon types (MON, OSD, MDS, RGW), C++ packages | Single binary, pure Go | MomoFS: radically simpler deployment |
| **Language** | C++ | Go | MomoFS: safer (memory), easier to contribute, cross-compilation |
| **Maturity** | 15+ years, exabyte-scale production | New, designed for thousands of nodes | Ceph: proven. MomoFS: unproven at scale |
| **Ecosystem** | OpenStack, Kubernetes (Rook), QEMU, librados (Python/C/Java) | S3-compatible (works with aws-cli, boto3, rclone) | Ceph: deep. MomoFS: standard S3 tooling |

**What MomoFS has that Ceph doesn't:**

| Capability | Description | Section |
|------------|-------------|---------|
| **Single binary** | One Go binary, zero external dependencies. No MON, MDS, OSD, RGW separate daemons. `./momo` and you have a full storage cluster. | DD-1 |
| **Polymorphic replication** | Runtime replication strategy switching based on CPU/memory load. Ceph requires manual PG remapping. | Existing `src/metrics/` |
| **QUIC transport** | Encrypted, multiplexed UDP transport. Ceph uses TCP only. | `src/transport/momo_quic.go` |
| **Chameleon routing** | Same port serves S3 clients and Momo peers simultaneously. Ceph needs separate RGW. | ARCHITECTURE.md |
| **AI-ready** | Vector embeddings, semantic search, content classification, intelligent tiering. Ceph has none of this. | Section 7 |
| **No MDS bottleneck** | Metadata distributed across all nodes via consistent hash ring. CephFS MDS can be a bottleneck for metadata-heavy workloads (small file creates, directory listings). | DD-1, Pillar 1.7 |
| **Embedded metadata** | BoltDB in-process, <0.1ms lookups. Ceph's MDS uses RocksDB but requires separate daemon + network hop. | DD-1, section 2.0.6 |
| **Zero-compaction** | BoltDB B+tree, no background compaction. Ceph's RocksDB/BlueStore requires compaction (CPU + I/O spikes). | Section 2.0.6 |

**What Ceph has that MomoFS doesn't (gaps):**

| Capability | Ceph | MomoFS | Gap level |
|------------|------|--------|-----------|
| **RBD (block storage)** | Mature, QEMU/OpenStack integration, thin provisioning, snapshots | Not planned | Intentional — MomoFS targets object/file, not block |
| **Kernel filesystem client** | CephFS kernel client (in-tree, high performance) | FUSE only (user-space, lower perf) | Medium — FUSE is adequate for most workloads |
| **PG-based recovery** | Placement groups enable parallel, bounded recovery | Not yet implemented (Phase 2 scrub) | Temporary — Phase 2 closes this |
| **Snapshots** | CephFS directory snapshots, RBD snapshots, RADOS pool snapshots | Not in roadmap | Low — can add per-bucket snapshots |
| **POSIX ACLs** | Full POSIX ACL support | Not planned | Low — S3 ACLs/bucket policies instead |
| **SPDK/NVMe-oF** | BlueStore SPDK backend, NVMe-oF target | Not planned | Low — NVMe via local BlobStore is fast enough |
| **Production track record** | Exabyte-scale, 15+ years, thousands of deployments | New | Temporary — closes with usage |
| **Deep ecosystem** | Rook (K8s), OpenStack Cinder, QEMU, librados SDKs | S3-compatible tooling + planned CSI driver | Medium — CSI driver (Pillar 4.2) + S3 covers most use cases |

**Performance comparison (projected):**

| Metric | Ceph (BlueStore, NVMe) | MomoFS (BoltDB, NVMe) | Notes |
|--------|----------------------|----------------------|-------|
| Object read latency (local) | ~0.5-1ms | ~0.6ms (0.1ms BoltDB + 0.5ms NVMe) | Comparable |
| Object write latency (local) | ~1-2ms | ~1ms (0.1ms BoltDB + 0.5ms NVMe + fsync) | Comparable |
| Metadata lookup | ~1-3ms (MDS RPC + RocksDB) | ~0.1ms (in-process BoltDB) | MomoFS 10-30x faster (no MDS hop) |
| Cluster-wide list | Seconds (PG scan across OSDs) | ~10ms × shard owners (scatter-gather) | MomoFS faster (fewer RPCs) |
| Small file create (metadata-heavy) | ~2-5ms (MDS + journal) | ~1ms (BoltDB + metadata replication) | MomoFS faster (no MDS) |
| Large file read (striped) | N_stripes × disk_BW | N_stripes × disk_BW | Comparable (same striping model) |
| Concurrent reads (same file) | N_replicas × disk_BW | N_replicas × disk_BW (or cache_BW) | Comparable; MomoFS has blob cache |
| Failover time | ~seconds (PG peering) | ~milliseconds (SWIM + replica failover) | MomoFS faster (no PG re-peering) |
| Cluster expansion | Minutes (PG rebalance) | Seconds (ring update + background stream) | MomoFS faster (consistent hashing: K/N keys move) |

**Scale comparison:**

| Dimension | Ceph | MomoFS | Notes |
|-----------|------|--------|-------|
| Max nodes | 10,000+ OSDs | Thousands (designed) | MomoFS: P2P gossip limits (~1000 practical, can extend with hierarchical gossip) |
| Max data | Exabytes | Unlimited (blob backends can be S3) | MomoFS: data scale depends on blob backend |
| Max objects | Billions | ~1B (100GB metadata/node × 1000 nodes / 100B per object) | MomoFS: can increase with more nodes |
| Metadata per node | ~10-100GB (RocksDB) | ~10MB-1GB (BoltDB, sharded) | MomoFS: less per node, but scales with more nodes |
| Cluster map size | MB (PG map) | KB (node list + ring) | MomoFS: simpler map (no PGs) |

**Summary:**

MomoFS is comparable to Ceph for **object storage** and **distributed filesystem** workloads. The core architecture is the same: masterless, CRUSH placement, self-healing, erasure coding, multi-tenancy. MomoFS is simpler to deploy (single binary), faster for metadata operations (no MDS, in-process BoltDB), and adds capabilities Ceph doesn't have (polymorphic replication, QUIC, AI-ready, chameleon routing).

Ceph is more mature, has deeper ecosystem integration (Rook, OpenStack, QEMU, RBD), and is proven at exabyte scale. MomoFS is newer but architecturally sound — the same design principles that make Ceph work (CRUSH, embedded KV, self-healing) are present in MomoFS.

**MomoFS is not trying to replace Ceph for block storage (RBD) or kernel-mounted CephFS.** MomoFS targets object storage (S3), distributed file access (FUSE), HPC parallel I/O, and cloud-native deployments. For those use cases, MomoFS is a valid alternative to Ceph with a simpler deployment story.
