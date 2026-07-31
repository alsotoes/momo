# Fast Recovery, Erasure Coding & Directory Operations

## 9. Fast Recovery

### 9.1 Current State

- No journaling — BoltDB provides ACID transactions but no write-ahead log for replication recovery
- Node restart requires full BoltDB scan for raw_alloc recovery
- No incremental sync — a node rejoining the cluster has no way to catch up on missed writes

### 9.2 Target: Fast Recovery

#### Journaling
- **Write-ahead log (WAL)**: Every metadata write is appended to a durable WAL before applying to BoltDB
- **WAL replication**: WAL entries are replicated to M-1 metadata replica nodes
- **Crash recovery**: Replay WAL from last checkpoint — sub-second recovery for typical workloads

#### Incremental Sync
- **Vector clocks**: Each metadata entry tagged with vector clock for version tracking
- **Merkle tree**: Per-shard Merkle tree root for fast divergence detection between replicas
- **Sync protocol**: On rejoin, compare Merkle roots, exchange only divergent entries

#### Parallel Rebuild
- **Sharded recovery**: Each metadata shard recovers independently in parallel
- **Prioritized recovery**: Hot/frequently-accessed objects recovered first
- **Background rebuild**: Node serves reads immediately from other replicas while rebuilding locally

#### Recovery Architecture

```
Node Rejoin Sequence:
1. Contact cluster via P2P gossip
2. Get current cluster map + metadata shard assignments
3. For each assigned shard:
   a. Compare local Merkle root with replica's root
   b. If divergent: exchange divergent entries (incremental sync)
   c. If empty: full copy from replica (parallel, sharded)
4. Replay local WAL for any writes after last sync point
5. Mark self as READY, start serving requests
6. Background: deep scrub to verify consistency
```

---

## 10. Erasure Coding

### 10.1 Current State

- Only full replicas (Chain, Splay) — no erasure coding
- Storage overhead = N×replication_factor

### 10.2 Target: Erasure Coding as a Replication Strategy

- **Reed-Solomon (k, m)**: Split object into k data chunks + m parity chunks
- **Placement**: Each chunk on a different node (CRUSH ensures rack/zone awareness)
- **Storage efficiency**: (k+m)/k overhead vs. N× for full replicas
- **Reads**: Read any k chunks to reconstruct — parallel reads from k nodes
- **Mode code**: `3` (alongside Chain=1, Splay=2)
- **Integration**: Implement as a new `Replicator` in `src/server/replication.go`

---

## 11. Cluster-Wide List and Directory Operations

### 11.1 Current State

- `List()` returns only local files — no cluster-wide listing
- No directory operations (mkdir, rmdir, rename)
- No recursive listing, no prefix-based listing

### 11.2 Target: Distributed Directory Service

- **Virtual filesystem**: Hierarchical namespace with `/`-delimited paths
- **Directory entries**: Stored in metadata shards (consistent hashing on path)
- **Cluster-wide List**: Scatter-gather query across all metadata shard owners
- **Pagination**: Cursor-based pagination for large listings
- **Operations**: mkdir, rmdir, rename (atomic, via lease consensus on the shard owner)

