# MomoFS Architecture — Masterless Ring

### 2.0 Architecture Summary

MomoFS is a **distributed masterless ring architecture** supporting multi-region replication and fault tolerance. No external database cluster is required — the Momo nodes themselves form the ring.

```
                    ┌─────────────────────────────────────────────┐
                    │           MomoFS Masterless Ring             │
                    │                                             │
                    │   Consistent Hash Ring (metadata sharding)   │
                    │   CRUSH Ring (data placement)                │
                    │   P2P Gossip + SWIM (membership)             │
                    │   Lease Consensus (quorum writes)            │
                    │                                             │
                    │   Each node = data server + metadata server  │
                    │   Embedded BoltDB (no external DB)           │
                    └─────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
          ┌─────────┐     ┌─────────┐     ┌─────────┐
          │ Node A  │     │ Node B  │     │ Node C  │
          │ shard 0 │     │ shard 1 │     │ shard 2 │
          │ +repl   │◄───►│ +repl   │◄───►│ +repl   │
          │ of 1,2  │     │ of 0,2  │     │ of 0,1  │
          └─────────┘     └─────────┘     └─────────┘
```

#### 2.0.1 Ring Topology

The ring is formed by consistent hashing. Each node is assigned `V` virtual nodes (vnodes) on the ring (default `V=150`), distributed uniformly via SHA-256. A key's owner is the node whose vnode is the first clockwise successor of `hash(key)`.

```
Hash space: 0 ─────────────────────────────────── 2^256 - 1

Virtual nodes (V=150 per physical node, 3 nodes shown):

  Node A vnodes:  ●     ●  ●        ●     ●  ●
  Node B vnodes:    ●  ●     ●  ●        ●  ●
  Node C vnodes: ●        ●     ●  ●  ●        ●

  Key "foo.txt" → hash = 0x4a92...
  → walk clockwise from 0x4a92... → first vnode = Node B
  → Node B owns metadata shard for "foo.txt"

  Replicas: next 2 vnodes clockwise = Node C, Node A
  → Metadata for "foo.txt" replicated to [B, C, A]
  → Quorum = 2 of 3
```

**Two independent rings:**

| Ring | Partitions | Algorithm | Replication Factor | Config |
|------|-----------|-----------|-------------------|--------|
| Metadata ring | File names → metadata shards | Consistent hashing (`HashRing`) | `metadata_replication` (default 3) | `[momofs]` |
| Data ring | Content hashes → blob placement | CRUSH weighted rendezvous hashing | `replication_factor` (existing, default 3) | `[global]` |

Separate rings because metadata and data have different optimization goals:
- **Metadata**: optimize for lookup speed + minimal remapping on cluster changes → consistent hashing
- **Data**: optimize for disk bandwidth + rack/zone awareness → CRUSH (weighted rendezvous hashing)

#### 2.0.2 Node Lifecycle in the Ring

```
Node joins:
  1. New node contacts any seed node via P2P gossip (existing SWIM join)
  2. Seed node shares cluster map + ring state
  3. New node is assigned V vnodes on the metadata ring
  4. Vnodes that "steal" keys from existing nodes are identified
  5. Previous owners stream those metadata shards to the new node (background)
  6. New node starts serving reads immediately (proxies if data not yet streamed)
  7. CRUSH map updated → data placement recalculated → new node gets data writes
  8. Background: data rebalances to new node (existing CRUSH reweight)

  Keys remapped: K/N (where N = total nodes) — minimal data movement
  (This is the key property of consistent hashing that makes scaling cheap)

Node leaves (graceful):
  1. Node sends Leave message via P2P gossip
  2. Ring updated → vnode ownership transfers to ring-adjacent nodes
  3. Metadata replicas rebalance: new owners pull from remaining replicas
  4. CRUSH map updated → data re-replicates to maintain replication factor
  5. Node drains in-flight requests, then exits
  6. No downtime — other nodes serve all reads during transition

Node leaves (ungraceful / crash):
  1. SWIM marks node SUSPECT after ping timeout (existing)
  2. After suspicion timeout → DEAD (existing)
  3. Ring excludes dead node's vnodes → replicas cover ownership
  4. Scrub thread detects under-replication → re-replicates (Phase 2)
  5. Reads: failover to next replica (Pillar 1.5)
  6. Writes: quorum = 2 of 3 → succeeds with 1 dead replica
```

#### 2.0.3 Data Flow Through the Ring

**Write path:**
```
Client: PUT /photos/sunset.jpg → Node C (any node)

  ┌─ Data ring (existing CRUSH) ──────────────────────────┐
  │ hash = sha256(content)                                 │
  │ dataTargets = CRUSH.Place(hash, replicationFactor=3)   │
  │ → [Node D, Node E, Node F]                             │
  │                                                        │
  │ Parallel stream to data targets:                       │
  │   Node D: PutBlob(hash, content) ── ack ✓             │
  │   Node E: PutBlob(hash, content) ── ack ✓             │
  │   Node F: PutBlob(hash, content) ── ack ✓             │
  │ Quorum: 2 of 3 ack → data write succeeds              │
  └────────────────────────────────────────────────────────┘

  ┌─ Metadata ring (new) ─────────────────────────────────┐
  │ shardKey = consistentHash("photos/sunset.jpg")         │
  │ owner = ring.Lookup(shardKey) → Node A                 │
  │ replicas = ring.Replicas(shardKey, M=3) → [A, B, G]   │
  │                                                        │
  │ Node C sends MetadataWrite RPC to Node A:              │
  │   {name: "photos/sunset.jpg",                          │
  │    hash: "abc123...", size: 2MB,                       │
  │    replicas: [D,E,F], vectorClock: [C:1]}              │
  │                                                        │
  │ Node A writes to local BoltDB (namespace + objects)    │
  │ Node A replicates to Node B (sync RPC) ── ack ✓       │
  │ Node A replicates to Node G (sync RPC) ── ack ✓       │
  │ Quorum: 2 of 3 → metadata write succeeds              │
  │ Node A returns ACK to Node C                           │
  └────────────────────────────────────────────────────────┘

  Node C returns success to client
  (Node C may not store any data or metadata — it just coordinated)
```

**Read path:**
```
Client: GET /photos/sunset.jpg → Node C (any node)

  ┌─ Metadata ring ───────────────────────────────────────┐
  │ shardKey = consistentHash("photos/sunset.jpg")         │
  │ owner = ring.Lookup(shardKey) → Node A                 │
  │                                                        │
  │ if Node C == Node A:                                   │
  │   meta = local BoltDB lookup (<0.1ms)                  │
  │ else:                                                  │
  │   meta = RPC(Node A, "ResolveMetadata", name) (~1ms)  │
  │   cache meta locally (TTL=60s)                         │
  └────────────────────────────────────────────────────────┘

  ┌─ Data ring ───────────────────────────────────────────┐
  │ if Node C has blob locally:                            │
  │   stream from local BlobStore (sub-ms NVMe)            │
  │ else:                                                  │
  │   best = min(RTT) from meta.replicas [D, E, F]        │
  │   stream from Node D via BlobProxy (~2ms)             │
  │   if D fails → try E → try F (Pillar 1.5 failover)    │
  │   cache blob if small + hot                            │
  └────────────────────────────────────────────────────────┘

  Stream to client
  (Client never knew data came from Node D via Node C)
```

**Delete path:**
```
Client: DELETE /photos/sunset.jpg → Node C (any node)

  1. Resolve metadata (same as read path) → Node A owns shard
  2. Node A writes tombstone to local BoltDB + replicates to [B, G]
  3. Node A decrements refcount on object (if dedup: only when RC=0)
  4. P2P gossip propagates tombstone to all nodes (existing mechanism)
  5. GC sweeps expired tombstones (existing `src/storage/gc.go`)
  6. If refcount=0: blob deleted from data targets [D, E, F]
```

#### 2.0.4 Consistency Model

MomoFS uses tunable consistency, similar to Cassandra's ONE/QUORUM/ALL:

| Operation | Default Consistency | Config | Rationale |
|-----------|-------------------|--------|-----------|
| Metadata write | QUORUM (2 of 3) | `metadata_quorum` | Prevents split-brain on concurrent writes |
| Metadata read | ONE (any replica) | — | Fast; read repair fixes stale replicas |
| Data write | QUORUM (2 of 3) | existing `replication_factor` | Durability without waiting for all replicas |
| Data read | ONE (local or proxy) | — | Fastest path; any replica has the data |
| Delete | QUORUM (metadata) + gossip (data) | — | Tombstone must be durable; blob GC is lazy |

**Conflict resolution**: Vector clocks per metadata entry. If two writes arrive concurrently (vector clocks are concurrent), last-writer-wins by timestamp. Scrub thread (Phase 2) detects and logs conflicts for manual review.

```
Concurrent write scenario:
  Node A: PutMetadata("foo.txt", vclock=[A:1]) → quorum [A,B]
  Node B: PutMetadata("foo.txt", vclock=[B:1]) → quorum [B,G]

  Both succeed (different quorums). On read:
    Node A returns [A:1], Node B returns [B:1]
    → Concurrent clocks → last-writer-wins by timestamp
    → Scrub logs conflict for review
    → Read repair propagates winning version to all replicas
```

#### 2.0.5 Multi-Region Topology

```
Region: us-east                          Region: eu-west
┌────────────────────────────────┐     ┌────────────────────────────────┐
│  MomoFS Ring (5 nodes)         │     │  MomoFS Ring (5 nodes)         │
│  ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐    │     │  ┌──┐ ┌──┐ ┌──┐ ┌──┐ ┌──┐    │
│  │A0│ │A1│ │A2│ │A3│ │A4│    │     │  │B0│ │B1│ │B2│ │B3│ │B4│    │
│  └──┘ └──┘ └──┘ └──┘ └──┘    │     │  └──┘ └──┘ └──┘ └──┘ └──┘    │
│  metadata RF=3 (local)         │     │  metadata RF=3 (local)         │
│  data RF=3 (local)             │     │  data RF=3 (local)             │
└─────────────┬──────────────────┘     └──────────────┬────────────────┘
              │                                       │
              └────────── cross-region link ─────────┘
                          (async, per-tenant)

Cross-region replication:
  - Async: writes commit locally, then replicate cross-region (RPO configurable)
  - Per-tenant: tenant opts in/out of geo-replication
  - Data residency: tenant data pinned to region (GDPR Article 44)
    → CRUSH placement restricted to nodes in tenant's allowed region
    → Metadata ring vnodes restricted to region nodes
  - Failover: DNS switch us-east.momo.io → eu-west.momo.io (RTO < 60s)
  - Conflict: vector clocks + last-writer-wins (same as intra-region)

  Each region has its own independent ring.
  Cross-region link is NOT part of the ring — it's a replication pipeline.
  This means:
    - Regions can fail independently (no cross-region quorum dependency)
    - Network partition between regions doesn't block local writes
    - Each region maintains full availability for its local tenants
```

#### 2.0.6 Storage Engine: BoltDB vs LSM-Tree

| Property | BoltDB (MomoFS) | LSM-Tree (Cassandra/ScyllaDB) |
|----------|----------------|-------------------------------|
| Structure | Single B+tree, mmap'd | Memtable + SSTable levels (L0-Ln) |
| Read latency | <0.1ms (1 tree traversal, mmap) | 0.5-5ms (memtable + SSTable scans) |
| Write latency | ~0.1ms (single B+tree insert + fsync) | ~0.1ms (memtable insert, async flush) |
| Write amplification | 1× (overwrite in place) | High (compaction rewrites) |
| Read amplification | 1× (single tree lookup) | Variable (bloom filter + SSTable reads) |
| Space amplification | Low (no duplicates) | High (until compaction) |
| Compaction | None needed | Background compaction (CPU + I/O intensive) |
| Range scan | Fast (B+tree cursor) | Fast (SSTable iterators) |
| Memory usage | mmap (OS manages page cache) | Explicit (memtable + block cache) |
| Max dataset size | ~100GB practical (single B+tree) | Petabytes (leveled compaction) |

**Why BoltDB is the right choice for MomoFS:**
- Metadata per node is small: 10MB-1GB (sharded across N nodes)
- Access pattern is KV lookup + prefix scan — BoltDB's B+tree is optimal
- No write amplification from compaction — lower CPU usage
- In-process — no network hop, no serialization overhead
- ACID transactions — BoltDB provides serializable isolation for free
- Zero dependencies — pure Go, single binary

**Where LSM-tree would win:**
- If metadata per node exceeded ~100GB (would need ~10PB cluster with 100M+ objects per node)
- If write-heavy workload with frequent overwrites (compaction absorbs writes better)
- Neither applies to MomoFS — metadata is small and mostly read-only

#### 2.0.7 Comparison with Cassandra/ScyllaDB

| Property | Cassandra/ScyllaDB | MomoFS | How |
|---|---|---|---|
| Masterless | All nodes equal, no coordinator | All nodes equal, no coordinator | P2P gossip (existing `src/p2p/gossip.go`) |
| Ring partitioning | Token ring partitions data | Consistent hash ring partitions metadata + CRUSH partitions data | `HashRing` (Pillar 1.7) + `ClusterMap.Placement()` (existing) |
| Replication | RF replicas per partition, ring-adjacent | M metadata replicas + N data replicas (independent factors) | Pillar 1.8 + existing CRUSH replication |
| Fault tolerance | Gossip failure detection, read repair | SWIM failure detection, read failover, scrub repair | Existing `src/p2p/peer_map.go` + Pillar 1.5 + Phase 2 scrub |
| Multi-region | DC-aware replication, RF per DC | Cross-cluster async replication, per-tenant region pinning | Pillar 4.4 |
| Consistency | Tunable consistency levels (ONE, QUORUM, ALL) | Quorum for writes, any-replica for reads | Lease consensus (existing) + Pillar 1.8 |
| Storage engine | LSM-tree (SSTables + memtable) | B-tree (BoltDB, mmap) | Embedded, in-process, <0.1ms lookups |
| Data model | Wide-column (partitions + clustering rows) | KV (name→hash, hash→ObjectMeta) | Simpler — no need for wide-column |
| Query language | CQL (SQL-like) | Go API + S3 REST | No query language needed — KV lookups |
| Secondary indexes | Built-in (SASI, materialized views) | BoltDB cursor scan + prefix index | Sufficient for List/filter operations |
| Transactions | LWT (lightweight transactions, Paxos) | Lease consensus (existing) | Quorum writes, vector clocks for conflicts |
| Deployment | Separate DB cluster + JVM (Cassandra) / C++ (ScyllaDB) | Single binary, embedded | Zero external dependencies |
| Ops burden | High (compaction tuning, GC, heap, repair) | None (BoltDB is self-managing) | No compaction, no GC, no heap tuning |

#### 2.0.8 Why No External Database

Momo's metadata is KV lookups (`name→hash`, `hash→ObjectMeta`), not wide-column queries. A 1PB cluster with 10M objects has ~1GB of metadata total. Sharded across 100 nodes = 10MB per node. BoltDB holds that entirely in memory (mmap). An external DB would add a network hop (1-5ms) for every metadata operation — 10-50x slower than BoltDB's in-process <0.1ms.

**The Momo cluster IS the database cluster.** Each Momo node is simultaneously:
- A **data server** (serves blobs via `BlobStore`)
- A **metadata server** (serves shards via `CASStore` + `MetadataResolver`)
- A **ring member** (participates in consistent hash ring + P2P gossip)
- A **coordinator** (can coordinate writes/reads for any key, even if it doesn't own the data)

This is the same architectural principle as Ceph: the storage nodes themselves form the distributed database. Ceph uses BlueStore/RocksDB (embedded KV) on each node. MomoFS uses BoltDB (embedded KV) on each node. Neither needs an external database cluster.

