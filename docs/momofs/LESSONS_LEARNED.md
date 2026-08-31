# Lessons Learned — What MomoFS Can Adopt from Ceph, Lustre, ScyllaDB, IPFS

This document identifies specific features and design patterns from each system that MomoFS should implement or take into consideration, organized by impact and effort.

---

## From Ceph

### 1. Per-Blob Checksums (BlueStore) — HIGH priority

Ceph's BlueStore stores a CRC32C checksum per blob and validates it on every read. This catches silent bitrot at the storage layer, before the client sees corrupt data.

**What MomoFS should do**: Extend `ObjectMeta` with a checksum field. Validate on every `GetBlob`. Mismatch → log, mark replica corrupt, failover to another replica.

```go
type ObjectMeta struct {
    Size      int64
    RefCount  int64
    DeletedAt int64
    Checksum  uint32  // CRC32C of blob content (new)
}
```

Effort: Low. Add checksum on `PutBlob`, verify on `GetBlob`. ~100 lines.

### 2. Throttled Background Recovery — MEDIUM priority

Ceph throttles PG backfill (`osd_max_backfills`) to limit client impact during recovery. Recovery runs in the background without degrading foreground I/O.

**What MomoFS should do**: Scrub and repair threads (Phase 2) should use a rate limiter — configurable bytes/sec and ops/sec. Foreground requests always take priority.

```toml
[scrub]
repair_rate_limit_mb = 50    # max 50MB/s repair traffic
repair_max_parallel = 4      # max 4 concurrent repair ops
repair_priority = "low"      # never starve foreground
```

### 3. Snapshots (Per-Bucket) — MEDIUM priority

Ceph supports per-pool, per-RBD-image, and per-directory snapshots with copy-on-write. MomoFS has no snapshots.

**What MomoFS should do**: Per-bucket snapshots using content-addressing. A snapshot is just a saved namespace mapping (name→hash at time T). Since blobs are immutable (CAS), no data copy is needed — just save the namespace pointers.

```
Snapshot "backup-2026-01" of bucket "photos":
  → Copy all namespace entries to snapshot bucket
  → Key: "snapshot:backup-2026-01:photos:sunset.jpg"
  → Val: same hash (blob not copied, just referenced)
  → Restore: swap namespace entries back from snapshot
```

Effort: Medium. Namespace copy + restore logic. BoltDB transaction.

### 4. Object Versioning — MEDIUM priority

Ceph RGW supports object versioning — each PUT creates a new version, old versions retained in a version bucket.

**What MomoFS should do**: Since blobs are content-addressed (immutable), versioning is natural. Each PUT to a versioned bucket keeps the old hash in a version chain.

```
PUT /report.doc (v1, hash=aaa) → namespace["report.doc"] = "aaa"
PUT /report.doc (v2, hash=bbb) → namespace["report.doc"] = "bbb"
                             versions["report.doc:1"] = "aaa" (timestamp)
DELETE /report.doc              → tombstone (versions retained)
Restore v1                      → namespace["report.doc"] = "aaa"
```

### 5. Inline Small Files (Data-on-MDT) — HIGH priority

Lustre stores small files (few MB) inline in the metadata server (on fast flash). This eliminates a separate data lookup for small files — massive latency win.

**What MomoFS should do**: Store blobs smaller than a threshold (e.g., 4KB) inline in BoltDB. The `objects` bucket value includes the blob bytes directly. `Get` for small files returns data from BoltDB — no BlobStore access needed.

```go
type ObjectMeta struct {
    Size      int64
    RefCount  int64
    DeletedAt int64
    Checksum  uint32
    InlineData []byte  // present if Size <= inlineThreshold (e.g., 4KB)
```

```
Read path for small file (1KB):
  Current:  BoltDB lookup (0.1ms) → BlobStore read (0.5ms) = 0.6ms
  With DoM: BoltDB lookup (0.1ms) → done. Data is in the metadata. = 0.1ms

  6× faster for small files. Most files in object storage are small.
```

Effort: Low. Extend ObjectMeta encoding, modify Put/Get paths. ~150 lines.

### 6. Object Pinning — HIGH priority

IPFS uses pinning to protect content from garbage collection. Pinned content is never evicted, even if refcount drops to zero. This is critical for compliance, legal holds, and "important" data.

**What MomoFS should do**: Add a `pinned` flag to `ObjectMeta`. Pinned objects are never GC'd regardless of refcount. Pinning can be per-object, per-bucket, or per-tenant.

```go
type ObjectMeta struct {
    // ...
    Pinned bool  // if true, GC never deletes this blob
```

```
Use cases:
  - Legal hold: pin object → survives Delete, survives GC
  - System data: pin bucket metadata, cluster config
  - Compliance: pin all objects in a regulated tenant
  - Cache warming: pin hot objects on edge nodes
```

Effort: Low. Add flag, check in GC. ~50 lines.

Ceph throttles PG backfill (`osd_max_backfills`) to limit client impact during recovery. Recovery runs in the background without degrading foreground I/O.

**What MomoFS should do**: Scrub and repair threads (Phase 2) should use a rate limiter — configurable bytes/sec and ops/sec. Foreground requests always take priority.

```toml
[scrub]
repair_rate_limit_mb = 50    # max 50MB/s repair traffic
repair_max_parallel = 4      # max 4 concurrent repair ops
repair_priority = "low"      # never starve foreground
```

### 3. Snapshots (Per-Bucket) — MEDIUM priority

Ceph supports per-pool, per-RBD-image, and per-directory snapshots with copy-on-write. MomoFS has no snapshots.

**What MomoFS should do**: Per-bucket snapshots using content-addressing. A snapshot is just a saved namespace mapping (name→hash at time T). Since blobs are immutable (CAS), no data copy is needed — just save the namespace pointers.

```
Snapshot "backup-2026-01" of bucket "photos":
  → Copy all namespace entries to snapshot bucket
  → Key: "snapshot:backup-2026-01:photos:sunset.jpg"
  → Val: same hash (blob not copied, just referenced)
  → Restore: swap namespace entries back from snapshot
```

Effort: Medium. Namespace copy + restore logic. BoltDB transaction.

### 4. Object Versioning — MEDIUM priority

Ceph RGW supports object versioning — each PUT creates a new version, old versions retained in a version bucket.

**What MomoFS should do**: Since blobs are content-addressed (immutable), versioning is natural. Each PUT to a versioned bucket keeps the old hash in a version chain.

```
PUT /report.doc (v1, hash=aaa) → namespace["report.doc"] = "aaa"
PUT /report.doc (v2, hash=bbb) → namespace["report.doc"] = "bbb"
                                 versions["report.doc:1"] = "aaa" (timestamp)
DELETE /report.doc              → tombstone (versions retained)
Restore v1                      → namespace["report.doc"] = "aaa"
```

### 5. Watch/Notify on Objects — LOW priority

Ceph supports watch/notify — clients subscribe to object changes and get notified. Useful for distributed coordination and cache invalidation.

**What MomoFS should do**: P2P-based watch/notify. When metadata changes on a shard owner, notify all nodes that have cached that metadata to invalidate their cache entries. This solves the stale cache problem without short TTLs.

### 6. PG Autoscaler (Auto-Sharding) — LOW priority

Ceph dynamically adjusts PG count per pool as data grows. MomoFS has a fixed shard count (256, configurable).

**What MomoFS should do**: Monitor metadata size per shard. When a shard exceeds a threshold, split it into two. This is consistent-harding-friendly (split = add a vnode). Not urgent — 256 shards handles ~25M objects per node comfortably.

---

## From Lustre

### 7. Data-on-MDT (Inline Small Files) — HIGH priority

Lustre stores small files (few MB) inline in the metadata server (on fast flash). This eliminates a separate data lookup for small files — massive latency win.

**What MomoFS should do**: Store blobs smaller than a threshold (e.g., 4KB) inline in BoltDB. The `objects` bucket value includes the blob bytes directly. `Get` for small files returns data from BoltDB — no BlobStore access needed.

```go
type ObjectMeta struct {
    Size      int64
    RefCount  int64
    DeletedAt int64
    Checksum  uint32
    InlineData []byte  // present if Size <= inlineThreshold (e.g., 4KB)
}
```

```
Read path for small file (1KB):
  Current:  BoltDB lookup (0.1ms) → BlobStore read (0.5ms) = 0.6ms
  With DoM: BoltDB lookup (0.1ms) → done. Data is in the metadata. = 0.1ms

  6× faster for small files. Most files in object storage are small.
```

Effort: Low. Extend ObjectMeta encoding, modify Put/Get paths. ~150 lines.

### 8. Progressive File Layout (PFL) — MEDIUM priority

Lustre's PFL changes file layout by offset region: small file → inline on MDT, growing → striped across OSTs. The layout evolves automatically as the file grows.

**What MomoFS should do**: Start every file inline (DoM). When size exceeds inline threshold, spill to BlobStore. When size exceeds stripe threshold, switch to striped layout. Metadata records the current layout — readers adapt automatically.

```
File grows: 0 → 4KB (inline) → 4KB-64MB (single blob) → 64MB+ (striped)
  Each transition is transparent — metadata records the layout.
  No client-side changes needed.
```

### 9. LDLM (Distributed Lock Manager) — MEDIUM priority

Lustre uses LDLM for coherent concurrent read/write with POSIX semantics. Extent locks allow multiple readers but exclusive writers per byte range.

**What MomoFS should do**: For FUSE mount (POSIX semantics), implement distributed locking via lease consensus. Lock at the file level (not extent level — simpler, sufficient for most workloads). Readers get shared lock, writers get exclusive lock. Locks held briefly (during operation), with renewal for long operations.

### 10. Persistent Client Cache (PCC) — MEDIUM priority

Lustre's PCC uses client-side NVMe/NVRAM as a persistent cache that stays in the global namespace. Files cached locally survive client restarts.

**What MomoFS should do**: Client-side persistent cache (on local disk, not just memory). Cache validated against metadata version (vector clock). If metadata hasn't changed, serve from local disk — zero network I/O.

```
Client cache hierarchy:
  1. Memory LRU (fastest, volatile) — existing ReadCache
  2. Local disk persistent cache (fast, survives restart) — new
  3. Remote cluster (slowest, always available) — BlobProxy
```

### 11. LNet Multi-Rail — LOW priority

Lustre bonds multiple NICs for aggregate bandwidth and network health. If one NIC fails, traffic continues on others.

**What MomoFS should do**: Support multiple network interfaces per node. QUIC connections can bond multiple paths. This is mostly an OS-level concern (bonding), but we should ensure our connection pool distributes across interfaces.

### 12. HSM (Hierarchical Storage Management) — LOW priority

Lustre's HSM provides policy-driven tiering to/from archive (tape, S3, another FS). More sophisticated than simple hot/warm/cold tiering.

**What MomoFS should do**: Policy engine for data lifecycle. Rules like "move objects not accessed in 90 days to S3 backend" or "delete objects older than 7 years unless legal hold." This extends our Phase 8 tiering with policy rules.

---

## From ScyllaDB

### 13. Hinted Handoff — HIGH priority

ScyllaDB stores writes to down replicas as "hints" on the coordinator. When the replica recovers, hints are replayed. This ensures writes succeed even during partial failures, without losing data.

**What MomoFS should do**: When a metadata replica is down during a write, the shard owner stores a hint (the write payload) locally. A background goroutine periodically tries to replay hints to recovered nodes.

```go
type Hint struct {
    TargetNode int       // node that was down
    Key        string    // metadata key
    Value      []byte    // metadata value
    Timestamp  time.Time // when the write happened
}
```

```
Write "foo.txt" → metadata replicas [A, B, G]
  Node A: write ✓
  Node B: write ✓ (quorum met)
  Node G: DOWN → store hint on Node A

  Later, Node G recovers:
  Node A replays hint to Node G → Node G now has the metadata
  Hint deleted from Node A
```

Effort: Low. Store hints in a BoltDB bucket, background replay goroutine. ~200 lines.

### 14. Per-Request Tunable Consistency — MEDIUM priority

ScyllaDB allows each read/write to specify consistency level (ONE, QUORUM, ALL, LOCAL_QUORUM). MomoFS currently has cluster-wide consistency.

**What MomoFS should do**: Add consistency level to the `Store` interface (optional, defaults to QUORUM):

```go
type ConsistencyLevel int
const (
    ONE ConsistencyLevel = iota    // any single replica
    QUORUM                         // majority of replicas
    ALL                            // all replicas
)

// Extended Store interface (optional, backward compatible)
type DistributedStore struct {
    // ...
    defaultCL ConsistencyLevel // cluster default (QUORUM)
}

// Per-operation override via context
ctx = WithConsistency(ctx, ALL)
store.Get(ctx, "critical-file.dat")
```

Use cases: `ALL` for critical metadata writes, `ONE` for fast cache lookups, `LOCAL_QUORUM` for region-local operations.

### 15. Phi-Accrual Failure Detector — MEDIUM priority

ScyllaDB uses phi-accrual failure detection (from Cassandra) — an adaptive, statistical approach that adjusts suspicion level based on heartbeat arrival time distribution. More accurate than fixed timeouts.

**What MomoFS should do**: Our SWIM already has adaptive RTT-based timeouts, but phi-accrual is more sophisticated. Consider upgrading the failure detector to phi-accrual for better accuracy in heterogeneous latency environments.

### 16. Materialized Views — LOW priority

ScyllaDB maintains materialized views — pre-computed secondary indexes updated on every write. Enables fast queries without full scans.

**What MomoFS should do**: For search (Phase 6), maintain secondary indexes in BoltDB. E.g., a `by_size` bucket mapping size ranges to file names, or `by_modified` mapping timestamps to file names. Updated on every Put. Enables `List(size_gt=1MB)` without scanning all keys.

### 17. CDC (Change Data Capture) — LOW priority

ScyllaDB's CDC streams every write to external systems (Kafka, etc.) per-shard. Enables event-driven architectures.

**What MomoFS should do**: Expose the WAL (Phase 6) as an event stream. Clients can subscribe to changes — useful for cache invalidation, audit logging, analytics pipelines, search index updates.

---

## From IPFS

### 18. Pinning as First-Class Concept — HIGH priority

IPFS uses pinning to protect content from garbage collection. Pinned content is never evicted, even if refcount drops to zero. This is critical for compliance, legal holds, and "important" data.

**What MomoFS should do**: Add a `pinned` flag to `ObjectMeta`. Pinned objects are never GC'd regardless of refcount. Pinning can be per-object, per-bucket, or per-tenant.

```go
type ObjectMeta struct {
    // ...
    Pinned bool  // if true, GC never deletes this blob
}
```

```
Use cases:
  - Legal hold: pin object → survives Delete, survives GC
  - System data: pin bucket metadata, cluster config
  - Compliance: pin all objects in a regulated tenant
  - Cache warming: pin hot objects on edge nodes
```

Effort: Low. Add flag, check in GC. ~50 lines.

### 19. Cooperative Caching — MEDIUM priority

IPFS's Bitswap protocol has cooperative caching — peers cache blocks they fetch and serve them to other peers. A block fetched by Node C from Node B becomes available at Node C for Node D to fetch.

**What MomoFS should do**: Our blob cache already caches proxied blobs (section 1.3a). But we can make it explicitly cooperative: when Node C caches a blob, it announces availability via P2P gossip. Other nodes can then fetch from Node C (a cache hit) instead of the original replica.

```
Current: Node D fetches blob from Node 42 (original replica, disk read)
Cooperative: Node D fetches blob from Node 13 (cached copy, memory read)
  → Node 13's cache serves as an additional "replica"
  → Reduces load on original replicas
  → Hot data naturally spreads to many cache nodes
```

This is partially already happening (our blob cache caches proxied reads), but we should make it explicit — advertise cached blobs in gossip, select cache nodes in replica selection.

### 20. CAR Files (Portable Archive) — MEDIUM priority

IPFS's CAR files serialize a set of content-addressed blocks into a single portable file. Used for backup, export, and sneakernet transfer.

**What MomoFS should do**: Export/import buckets as CAR-like archives. Since our blobs are content-addressed (SHA-256), this is natural. A CAR file is just a tar of (hash, metadata, blob data) tuples.

```
Export bucket "photos" to photos.car:
  For each file in bucket:
    Write (name, hash, size, blob bytes) to archive
  Verify: recompute SHA-256 of each blob, compare to hash

Import photos.car to new cluster:
  For each entry:
    PutBlob(hash, bytes) → stored on CRUSH targets
    PutMetadata(name, hash) → stored on shard owner
  No re-hashing needed — hashes are verified during import
```

Useful for: backup/restore, cluster migration, sneakernet, GDPR data portability (Article 20).

### 21. Graphsync (Efficient DAG Sync) — LOW priority

IPFS's Graphsync only fetches missing blocks when syncing a DAG. If two nodes have most of a dataset, only the missing blocks are transferred.

**What MomoFS should do**: For incremental sync (Phase 6, node rejoin), use a similar approach. Compare Merkle roots per shard. Only transfer the divergent entries. This is already in our Phase 6 design — Graphsync validates the approach.

### 22. IPNS (Mutable Names for Immutable Content) — LOW priority

IPFS uses IPNS to create mutable names that point to immutable content. Updating a name publishes a new CID. This enables versioning without copying.

**What MomoFS should do**: This is essentially what our namespace bucket already does — `name → hash`, and updating the name points to a new hash. But we could expose this as a first-class versioning API: `SetPointer(name, hash)`, `GetPointer(name)`, `History(name)`.

---

## Priority Summary

### Implement Now (Phase 1-2, High Impact, Low Effort)

| # | Feature | From | Effort | Impact |
|---|---------|------|--------|--------|
| 1 | Per-blob checksums (CRC32C) | Ceph | ~100 lines | Catches bitrot before client sees corrupt data |
| 7 | Inline small files (Data-on-MDT) | Lustre | ~150 lines | 6× faster reads for small files (most files) |
| 13 | Hinted handoff | ScyllaDB | ~200 lines | No data loss during transient node failures |
| 18 | Pinning (GC protection) | IPFS | ~50 lines | Legal holds, compliance, system data protection |

### Implement Soon (Phase 2-4, Medium Impact)

| # | Feature | From | Effort | Impact |
|---|---------|------|--------|--------|
| 2 | Throttled background recovery | Ceph | Low | Recovery doesn't impact foreground I/O |
| 3 | Per-bucket snapshots | Ceph | Medium | Backup/restore without data copy (CAS) |
| 4 | Object versioning | Ceph | Medium | Version history, restore |
| 8 | Progressive file layout | Lustre | Medium | Auto-inline → blob → striped as file grows |
| 14 | Per-request consistency level | ScyllaDB | Low | Critical writes use ALL, fast reads use ONE |
| 19 | Cooperative caching | IPFS | Medium | Hot data spreads, reduces replica load |
| 20 | CAR files (portable archive) | IPFS | Medium | Backup, migration, GDPR portability |

### Consider Later (Phase 5-8, Lower Priority)

| # | Feature | From | Effort | Impact |
|---|---------|------|--------|--------|
| 5 | Watch/notify (cache invalidation) | Ceph | Medium | Solves stale cache without short TTLs |
| 6 | Auto-sharding (dynamic shard count) | Ceph | Medium | Self-tuning as cluster grows |
| 9 | Distributed lock manager | Lustre | Medium | POSIX coherence for FUSE mount |
| 10 | Persistent client cache | Lustre | Medium | Client-side NVMe cache, survives restart |
| 15 | Phi-accrual failure detector | ScyllaDB | Low | Better failure detection in heterogeneous latency |
| 16 | Materialized views (secondary indexes) | ScyllaDB | Medium | Fast filtered queries without full scan |
| 17 | CDC (change data capture) | ScyllaDB | Medium | Event-driven architectures, audit, analytics |
| 21 | Graphsync (efficient DAG sync) | IPFS | Medium | Faster node rejoin, less bandwidth |
| 12 | HSM (policy-driven tiering) | Lustre | Medium | Automated lifecycle rules |

### Not Worth Implementing

| Feature | From | Why |
|---------|------|-----|
| RDMA / InfiniBand | Ceph, Lustre | Niche HPC interconnect; TCP/QUIC sufficient for most deployments |
| GPU Direct Storage | Lustre | Very niche; not our target |
| Kernel POSIX client | Ceph, Lustre | Would need kernel module (C); FUSE is sufficient |
| Seastar thread-per-core | ScyllaDB | Go's goroutine M:N scheduler achieves similar utilization |
| LWT (Paxos per key) | ScyllaDB | Quorum + LWW is sufficient; Paxos adds 2-3× write latency |
| Filecoin / incentivized storage | IPFS | Different paradigm (economic incentives) |
| IPLD / cross-system DAG | IPFS | Different data model; our CAS is simpler |
| Block device (RBD) | Ceph | Intentional — we target object + file, not block |
| Wide-column / CQL | ScyllaDB | BoltDB KV is sufficient for our metadata |
