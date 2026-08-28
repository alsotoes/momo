# Implementation Design — The Four Pillars

## 2.5 The Four Pillars — Implementation Design

The vision above describes *what* MomoFS achieves. This section designs *how* — the concrete Go interfaces, protocols, data flows, and configuration that make the four customer-facing pillars real. Everything here is grounded in the existing codebase: `BlobStore`, `CASStore`, `ClusterMap.Placement()`, `ScatterGather`, P2P gossip, and the `Store` interface.

### Existing Infrastructure We Build On

| Component | Location | What It Provides |
|-----------|----------|------------------|
| `BlobStore` interface | `src/storage/blobstore.go` | Pluggable blob backends (local, S3, raw, NFS) |
| `CASStore` | `src/storage/storage.go` | Local BoltDB metadata (4 buckets: objects, namespace, paths, tombstones) |
| `Store` interface | `src/storage/storage.go` | Put/Get/Has/Delete/List — the current API |
| `ClusterMap.Placement()` | `src/common/crush.go` | CRUSH weighted rendezvous hashing — deterministic data placement |
| `ScatterGather` | `src/p2p/scatter_gather.go` | P2P broadcast query to all peers, collect responses |
| `QueryHandler` | `src/p2p/scatter_gather.go` | Handle query types: List, Get, Has, Delete |
| `StorageQueryHandler` | `src/server/query_handler.go` | Current scatter-gather handler over local store |
| P2P Gossip + SWIM | `src/p2p/gossip.go`, `src/p2p/peer_map.go` | Membership, failure detection, lease consensus |
| `Configuration` | `src/common/struct.go` | Global, Metrics, P2P, Storage config sections |

**What's missing**: Metadata is local-only. `Store.Get(name)` only checks the local `namespace` bucket. There's no way for Node C to find a file written to Node A unless C is a data replica. Scatter-gather asks *all* nodes — it works but doesn't scale (O(N) per query). We need targeted metadata resolution: ask the *one* node that owns the metadata shard.

---

### Pillar 1: Read From Any Node — Implementation

#### 1.1 Metadata Resolution Protocol

When any node receives a read request, it resolves metadata by computing which node owns the metadata shard, then asks that node (or serves locally if it *is* the owner).

```
Request: GET /photos/sunset.jpg → Node C (any node)

Step 1: Compute metadata shard owner
  shardKey = consistentHash("/photos/sunset.jpg", numShards)
  shardOwner = ring.Lookup(shardKey)  →  Node A

Step 2: Resolve metadata
  if Node C == Node A:
    meta = local BoltDB lookup  (sub-ms)
  else:
    meta = RPC(Node A, "ResolveMetadata", "/photos/sunset.jpg")  (~1ms)

Step 3: Check local blob
  exists = s.blobs.Has(meta.hash)

  if exists:
    stream from local BlobStore  (sub-ms NVMe, ~2ms SSD)
  else:
    select best replica = min(RTT) from meta.replicas
    stream from replica node via BlobProxy  (RTT + disk)

Step 4: Cache
  cache metadata (TTL=60s) → next request skips RPC
  if blob is small (<4MB) and hot: cache blob bytes → next read is local
```

#### 1.1a Concrete Walkthrough: 100 Nodes, Replica 3, Read From Any

This traces the exact flow for the question: *"100 server nodes, write a file with replica 3 (only 3 nodes store the data), then a client wants to read it — how does the client know where the file is?"*

**The client never needs to know where the file is.** The client talks to any node. That node finds the file for them.

```
Cluster: 100 nodes (Node 0 .. Node 99)
Data replication factor: 3
Metadata replication factor: 3
Metadata shards: 256
Hash ring: 100 nodes × 150 vnodes each = 15,000 vnodes on the ring

═══════════════════════════════════════════════════════════════════════
WRITE: Client writes "report.pdf" (2MB)
═══════════════════════════════════════════════════════════════════════

Client → PUT /report.pdf → Node 55 (any node, picked by load balancer)

Step 1: Node 55 hashes the content
  hash = sha256(content) = "a1b2c3d4..."

Step 2: Node 55 computes data placement via CRUSH (existing code)
  dataTargets = ClusterMap.Placement("a1b2c3d4...", replicationFactor=3)
  → [Node 7, Node 42, Node 88]

  Only these 3 nodes will store the blob bytes.
  The other 97 nodes have no idea this blob exists.

Step 3: Node 55 streams blob to the 3 data targets (parallel)
  ├── Node 7:  PutBlob("a1b2c3d4...", content) → ack ✓
  ├── Node 42: PutBlob("a1b2c3d4...", content) → ack ✓
  └── Node 88: PutBlob("a1b2c3d4...", content) → ack ✓
  Quorum: 2 of 3 ack → data write succeeds

Step 4: Node 55 computes metadata shard owner
  shardKey = consistentHash("report.pdf", 256 shards)
  = 0x4a92... (some value in hash space)
  shardOwner = ring.Lookup(0x4a92...) → Node 23

  Node 23 is the metadata authority for "report.pdf".
  It may or may not be one of the data targets [7, 42, 88].
  In this case, Node 23 is NOT a data target.

Step 5: Node 55 sends metadata to Node 23
  RPC(Node 23, "PutMetadata", {
    name:     "report.pdf",
    hash:     "a1b2c3d4...",
    size:     2097152,
    replicas: [7, 42, 88],     ← which nodes have the blob
    vclock:   [55:1]
  })

Step 6: Node 23 writes metadata to local BoltDB + replicates to ring-adjacent nodes
  Metadata replicas for this shard = ring.Replicas(shardKey, 3) → [Node 23, Node 67, Node 91]

  Node 23: BoltDB write
    namespace bucket:  "report.pdf" → "a1b2c3d4..."
    objects bucket:    "a1b2c3d4..." → {size=2MB, refCount=1, replicas=[7,42,88]}

  Node 23 → Node 67: sync metadata replicate → ack ✓
  Node 23 → Node 91: sync metadata replicate → ack ✓
  Quorum: 2 of 3 → metadata write succeeds

Step 7: Node 55 returns "200 OK" to client

  ┌─────────────────────────────────────────────────────────────┐
  │  STATE AFTER WRITE:                                         │
  │                                                             │
  │  Blob data (2MB each):                                      │
  │    Node 7:  ✓  (has blob "a1b2c3d4...")                    │
  │    Node 42: ✓  (has blob "a1b2c3d4...")                    │
  │    Node 88: ✓  (has blob "a1b2c3d4...")                    │
  │    Other 97 nodes: no blob data                             │
  │                                                             │
  │  Metadata (~200 bytes each):                                │
  │    Node 23:  ✓  (name→hash, hash→ObjectMeta{replicas:[7,42,88]}) │
  │    Node 67: ✓  (metadata replica)                           │
  │    Node 91: ✓  (metadata replica)                           │
  │    Other 97 nodes: no metadata for this file                │
  │                                                             │
  │  Client knows: nothing about where data or metadata is.     │
  │  Client only knows: "report.pdf" was written successfully.  │
  └─────────────────────────────────────────────────────────────┘

═══════════════════════════════════════════════════════════════════════
READ: Client (or a different client) wants to read "report.pdf"
═══════════════════════════════════════════════════════════════════════

Client → GET /report.pdf → Node 13 (ANY node — load balancer picks randomly)

  Node 13 does NOT have the blob.
  Node 13 does NOT have the metadata.
  Node 13 has NO IDEA where "report.pdf" is stored.

  ...but Node 13 can find it. Here's how:

Step 1: Node 13 checks metadata cache
  cache.GetMetadata("report.pdf") → MISS (first read)

Step 2: Node 13 computes metadata shard owner (same computation as write)
  shardKey = consistentHash("report.pdf", 256) = 0x4a92...
  shardOwner = ring.Lookup(0x4a92...) → Node 23

  Node 13 now knows: "Node 23 is the metadata authority for report.pdf"
  (This is a deterministic computation — no guessing, no scanning all nodes)

Step 3: Node 13 asks Node 23 for metadata
  RPC(Node 23, "ResolveMetadata", "report.pdf")

  Node 23 looks up local BoltDB:
    namespace["report.pdf"] → "a1b2c3d4..."
    objects["a1b2c3d4..."] → {size=2MB, replicas=[7, 42, 88]}

  Node 23 returns:
    {hash: "a1b2c3d4...", size: 2097152, replicas: [7, 42, 88]}

  Node 13 caches this: cache.PutMetadata("report.pdf", meta, TTL=60s)

Step 4: Node 13 checks — do I have the blob locally?
  local.Has("a1b2c3d4...") → NO (Node 13 is not in [7, 42, 88])

Step 5: Node 13 picks the best replica and streams from it
  Replica candidates: [Node 7, Node 42, Node 88]
  Best = min(RTT): Node 42 (1.2ms RTT, tracked by P2P gossip)

  Node 13 → Node 42: "Give me blob a1b2c3d4..."
  Node 42 streams 2MB to Node 13
  Node 13 streams to client

  (If Node 42 is down: try Node 7, then Node 88 — Pillar 1.5 failover)
  (If all 3 are down: return EIO — but this means 3 simultaneous failures)

Step 6: Node 13 caches small blob
  2MB < 4MB (blob_max_size_mb) → cache.PutBlob("a1b2c3d4...", data)
  Next read of same file → cache hit → no RPC, no proxy, sub-ms

  ┌─────────────────────────────────────────────────────────────┐
  │  CLIENT PERSPECTIVE:                                        │
  │                                                             │
  │  Client sent GET /report.pdf to Node 13.                    │
  │  Client received 2MB of data.                               │
  │  Client never knew:                                         │
  │    - which node had the blob (Node 42)                      │
  │    - which node had the metadata (Node 23)                  │
  │    - that Node 13 proxied the data from Node 42             │
  │    - that there are 100 nodes in the cluster                │
  │    - that the file has 3 replicas                           │
  │    - anything about CRUSH, hash rings, or shards            │
  │                                                             │
  │  Client just talked to an endpoint and got a file.          │
  │  That's "Read From Any Node."                               │
  │  That's "Customer-Transparent Complexity."                  │
  └─────────────────────────────────────────────────────────────┘
```

**What if the client connects to one of the 3 data nodes?**

```
Client → GET /report.pdf → Node 7 (happens to be a data replica)

Step 1-3: Same as above — Node 7 resolves metadata (maybe via RPC to Node 23)
Step 4: Node 7 checks — do I have the blob locally?
  local.Has("a1b2c3d4...") → YES!
Step 5: Stream directly from local BlobStore (sub-ms NVMe)
  → No proxy needed, no network hop, fastest possible path
```

**What if the client connects to the metadata shard owner?**

```
Client → GET /report.pdf → Node 23 (happens to be the shard owner)

Step 1-2: Same — but Node 23 IS the shard owner
Step 3: Local BoltDB lookup (no RPC needed, <0.1ms)
Step 4: Node 23 checks — do I have the blob locally?
  local.Has("a1b2c3d4...") → NO (Node 23 is not in [7, 42, 88])
Step 5: Proxy from Node 42 (same as the main example)
```

**What if the metadata shard owner (Node 23) is down?**

```
Client → GET /report.pdf → Node 13

Step 2: shardOwner = ring.Lookup(0x4a92...) → Node 23
Step 3: RPC(Node 23, ...) → TIMEOUT (Node 23 is down)

  → Fall back to metadata replicas: [Node 67, Node 91]
  → RPC(Node 67, "ResolveMetadata", "report.pdf") → success
  → Node 67 returns same metadata (it's a replica of the same shard)

  Meanwhile: SWIM marks Node 23 as SUSPECT
  → Scrub will re-replicate metadata to maintain 3 copies (Phase 2)
```

**What if a data replica (Node 42) is down?**

```
Client → GET /report.pdf → Node 13

Step 5: Try Node 42 → TIMEOUT
  → Mark Node 42 SUSPECT in SWIM
  → Try Node 7 → success → stream from Node 7
  → (Node 88 also available as third fallback)

  Meanwhile: scrub detects under-replication
  → CRUSH computes new target to replace Node 42
  → Re-replicates blob from Node 7 or Node 88 to new target (Phase 2)
```

**Second read of the same file (within cache TTL):**

```
Client → GET /report.pdf → Node 13 (same node, within 60s)

Step 1: cache.GetMetadata("report.pdf") → HIT!
  → Skip steps 2-3 (no RPC to Node 23)

Step 4: cache.GetBlob("a1b2c3d4...") → HIT! (cached from first read)
  → Skip steps 5-6 (no proxy from Node 42)

  → Stream directly from memory cache (0.02ms)
  → Total latency: 0.02ms (vs. ~3ms for first read)

  This is why the read cache matters — hot files become local.
```

**Different client on a different node reads the same file:**

```
Client B → GET /report.pdf → Node 79 (different node)

Step 1: cache.GetMetadata("report.pdf") → MISS (Node 79 hasn't seen this file)
Step 2: shardOwner = ring.Lookup(0x4a92...) → Node 23 (same computation, same result)
Step 3: RPC(Node 23, ...) → {hash: "a1b2c3d4...", replicas: [7, 42, 88]}
Step 4: local.Has("a1b2c3d4...") → NO
Step 5: Proxy from best replica (Node 7, 0.8ms RTT)
Step 6: Cache blob + metadata

  Node 79 now also has the file cached.
  With 100 nodes and a 256MB blob cache, hot files propagate to many nodes.
  Eventually, most reads are cache hits — the cluster "learns" hot data.
```

#### 1.2 Go Interface Design

These interfaces extend the existing `Store` and `BlobStore` without breaking them. The current `CASStore` continues to work for local-only deployments (`momofs.enabled = false`).

```go
// MetadataResolver resolves any file name to its metadata,
// regardless of which node holds the metadata shard.
// If this node owns the shard, reads from local BoltDB.
// Otherwise, RPCs the shard owner.
type MetadataResolver interface {
    Resolve(ctx context.Context, name string) (*ResolvedMeta, error)
    // ResolveBatch resolves multiple names in one RPC (for List operations).
    ResolveBatch(ctx context.Context, names []string) ([]*ResolvedMeta, error)
}

// ResolvedMeta is the result of metadata resolution.
type ResolvedMeta struct {
    Hash       string        // content hash (SHA-256 hex)
    Size       int64         // file size in bytes
    Replicas   []int         // node IDs that have the blob (from CRUSH)
    DeletedAt  time.Time     // zero = not deleted
    VectorClock []uint64     // for conflict resolution (Phase 1)
    RemotePath string        // virtual path (from paths bucket)
}

// BlobProxy streams a blob from a remote node when this node doesn't have it.
type BlobProxy interface {
    // FetchBlob streams blob data from the best available replica.
    // Tries replicas in RTT order, fails over on timeout/error.
    FetchBlob(ctx context.Context, hash string, replicas []int) (io.ReadCloser, error)
}

// ReadCache caches metadata and blobs locally to avoid repeated RPCs.
type ReadCache interface {
    GetMetadata(name string) (*ResolvedMeta, bool)
    PutMetadata(name string, meta *ResolvedMeta, ttl time.Duration)
    GetBlob(hash string) (io.ReadCloser, bool)
    PutBlob(hash string, data []byte)
    Stats() CacheStats // hit rate, evictions, memory usage
}

// DistributedStore implements Store but with cluster-wide metadata resolution.
// It composes the existing CASStore (local) with MetadataResolver + BlobProxy.
type DistributedStore struct {
    local    *CASStore           // existing local store (unchanged)
    resolver MetadataResolver   // new: shard-aware metadata lookup
    proxy    BlobProxy           // new: remote blob streaming
    cache    ReadCache           // new: LRU cache for meta + blobs
    ring     *HashRing           // new: consistent hash ring for shard ownership
    selfID   int                 // this node's ID
}
```

**Key design property**: `DistributedStore` implements the existing `Store` interface. The server daemon (`server.Daemon`) doesn't change — it calls `store.Get()` whether the store is local or distributed. The complexity is entirely internal.

#### 1.3 Read Path — DistributedStore.Get()

```go
func (d *DistributedStore) Get(name string) (io.ReadCloser, common.FileMetadata, error) {
    // 1. Check metadata cache
    if meta, ok := d.cache.GetMetadata(name); ok {
        if !meta.DeletedAt.IsZero() {
            return nil, common.FileMetadata{}, syscall.ENOENT
        }
        return d.serveBlob(meta, name)
    }

    // 2. Resolve metadata (local if shard owner, RPC otherwise)
    meta, err := d.resolver.Resolve(ctx, name)
    if err != nil {
        return nil, common.FileMetadata{}, err
    }

    // 3. Cache metadata
    d.cache.PutMetadata(name, meta, 60*time.Second)

    // 4. Check tombstone
    if !meta.DeletedAt.IsZero() {
        return nil, common.FileMetadata{}, syscall.ENOENT
    }

    // 5. Serve blob
    return d.serveBlob(meta, name)
}

func (d *DistributedStore) serveBlob(meta *ResolvedMeta, name string) (io.ReadCloser, common.FileMetadata, error) {
    // 5a. Check blob cache
    if cached, ok := d.cache.GetBlob(meta.Hash); ok {
        return cached, common.FileMetadata{Name: name, Hash: meta.Hash, Size: meta.Size, RemotePath: meta.RemotePath}, nil
    }

    // 5b. Check local store — if we have it locally, serve directly
    if exists, _ := d.local.Has(meta.Hash); exists {
        rc, _, err := d.local.Get(name)
        return rc, common.FileMetadata{Name: name, Hash: meta.Hash, Size: meta.Size, RemotePath: meta.RemotePath}, err
    }

    // 5c. PARALLEL MULTI-NODE READ (see section 1.3a)
    //     Split blob into chunks, fetch from ALL replicas in parallel.
    //     This is the default — not just for HPC. Every remote read is parallel.
    rc, err := d.proxy.FetchBlobParallel(ctx, meta.Hash, meta.Size, meta.Replicas)
    if err != nil {
        return nil, common.FileMetadata{}, err
    }

    // 5d. Cache small blobs
    if meta.Size < maxCacheableBlobSize {
        data, _ := io.ReadAll(rc)
        rc.Close()
        d.cache.PutBlob(meta.Hash, data)
        return io.NopCloser(bytes.NewReader(data)), common.FileMetadata{...}, nil
    }

    return rc, common.FileMetadata{Name: name, Hash: meta.Hash, Size: meta.Size, RemotePath: meta.RemotePath}, nil
}
```

#### 1.3a Parallel Multi-Node Read — Core Read Strategy

**Every remote read is parallel.** This is not an HPC-only feature — it's the default behavior. When a blob exists on N replica nodes, we split it into N chunks and fetch all chunks simultaneously from different nodes. This minimizes latency and maximizes aggregate bandwidth.

```
File: bigdata.parquet (300MB)
Replicas: [Node 7, Node 42, Node 88]  (3 full copies via CRUSH)
Request: GET /bigdata.parquet → Node 13 (doesn't have it locally)

OLD (single-node read — sequential):
  Node 13 → Node 42: stream 300MB  ───────────────────────────────►
  Time = 300MB / disk_BW  (e.g., 300MB / 700MB/s = 428ms)
  Bandwidth = 1 × disk_BW

NEW (parallel multi-node read — 3 nodes simultaneously):
  Split into 3 chunks of 100MB each:
  ├── goroutine 1: Node 13 → Node 7:  Range 0-100MB     ──►  (parallel)
  ├── goroutine 2: Node 13 → Node 42: Range 100-200MB  ──►  (parallel)
  └── goroutine 3: Node 13 → Node 88: Range 200-300MB  ──►  (parallel)

  Merge in order → stream to client
  Time = 100MB / disk_BW  (e.g., 100MB / 700MB/s = 143ms)  ← 3× faster
  Bandwidth = 3 × disk_BW  (2.1 GB/s aggregate)
```

**Why this matters:**

| Metric | Single-Node Read | Parallel Multi-Node Read (3 replicas) | 10 replicas |
|--------|-----------------|--------------------------------------|-------------|
| Latency (300MB file) | 428ms | 143ms (3× faster) | 43ms (10× faster) |
| Aggregate bandwidth | 1 × disk_BW | 3 × disk_BW | 10 × disk_BW |
| Network utilization | 1 link saturated | 3 links utilized | 10 links utilized |
| Disk I/O per node | 1 node does all work | Work spread across 3 nodes | Work spread across 10 nodes |
| Tail latency | Determined by 1 node | Determined by slowest of 3 chunks (smaller chunks = lower tail) | Even smaller chunks |

**The more replicas, the faster the read.** This is why `metadata_replication` and `replication_factor` directly impact read performance — not just durability. Adding replicas increases both fault tolerance AND read throughput.

**Go Interface:**

```go
// BlobProxy streams a blob from remote nodes when this node doesn't have it.
type BlobProxy interface {
    // FetchBlobParallel splits the blob into len(replicas) chunks and
    // fetches each chunk from a different replica in parallel.
    // Returns a merged io.ReadCloser that streams chunks in order.
    FetchBlobParallel(ctx context.Context, hash string, size int64, replicas []int) (io.ReadCloser, error)

    // FetchBlobRange fetches a specific byte range from one node.
    // Used internally by FetchBlobParallel for each parallel chunk.
    FetchBlobRange(ctx context.Context, hash string, offset int64, length int64, nodeID int) (io.ReadCloser, error)
}
```

**Implementation:**

```go
func (p *blobProxy) FetchBlobParallel(ctx context.Context, hash string, size int64, replicas []int) (io.ReadCloser, error) {
    nReplicas := len(replicas)
    chunkSize := size / int64(nReplicas)

    // Launch one goroutine per replica, each fetching a different chunk
    type chunkResult struct {
        index int
        data   []byte
        err    error
    }

    results := make(chan chunkResult, nReplicas)
    for i, nodeID := range replicas {
        offset := int64(i) * chunkSize
        length := chunkSize
        if i == nReplicas-1 {
            length = size - offset // last chunk gets remainder
        }

        go func(idx int, node int, off, len int64) {
            rc, err := p.FetchBlobRange(ctx, hash, off, len, node)
            if err != nil {
                results <- chunkResult{index: idx, err: err}
                return
            }
            defer rc.Close()
            data, err := io.ReadAll(rc)
            results <- chunkResult{index: idx, data: data, err: err}
        }(i, nodeID, offset, length)
    }

    // Collect results in order, return as a single stream
    chunks := make([][]byte, nReplicas)
    for i := 0; i < nReplicas; i++ {
        r := <-results
        if r.err != nil {
            // Failover: re-fetch this chunk from another replica
            chunks[r.index] = p.refetchChunk(ctx, hash, r.index, chunkSize, replicas)
        } else {
            chunks[r.index] = r.data
        }
    }

    return io.NopCloser(newChunkReader(chunks)), nil
}
```

**Streaming variant (for large files — don't buffer in memory):**

```go
func (p *blobProxy) FetchBlobParallelStream(ctx context.Context, hash string, size int64, replicas []int) (io.ReadCloser, error) {
    // Use io.Pipe so chunks stream to client as they arrive, not after all complete.
    // A background goroutine writes chunks in order to the pipe.
    // Chunk i starts streaming as soon as chunks 0..i-1 have streamed,
    // even if chunks i+1..N are still in flight.

    pr, pw := io.Pipe()
    go func() {
        // ... fetch chunks in parallel, write to pw in order ...
        // ... chunk i can start writing as soon as it arrives, if 0..i-1 are done ...
    }()
    return pr, nil
}
```

**Adaptive chunk count:**

```go
// The number of parallel chunks adapts to file size and replica count:
func parallelChunkCount(size int64, replicas int) int {
    if size < 1*MB {
        return 1  // tiny file: not worth the overhead of parallel reads
    }
    if size < 10*MB {
        return min(2, replicas)  // small file: 2 parallel reads
    }
    // large file: use all replicas
    return replicas
}
```

**Interaction with striping (Pillar 3.1):**

Striping and parallel multi-node reads compose:

```
Striped file: 1TB, 4 stripes, each stripe has 3 replicas

Stripe 0 (256MB) on [A, B, C]  →  3 parallel range reads (85MB each)
Stripe 1 (256MB) on [D, E, F]  →  3 parallel range reads (85MB each)
Stripe 2 (256MB) on [G, H, I]  →  3 parallel range reads (85MB each)
Stripe 3 (256MB) on [J, K, L]  →  3 parallel range reads (85MB each)

Total: 12 parallel reads from 12 nodes
Aggregate bandwidth: 12 × disk_BW
Time: 1TB / (12 × disk_BW)  (e.g., 1TB / 8.4GB/s = 119s)
```

Without striping, a 1TB file with 3 replicas:
```
3 parallel range reads, each fetching 333MB
Aggregate bandwidth: 3 × disk_BW
Time: 1TB / (3 × disk_BW)  (e.g., 1TB / 2.1GB/s = 476s)
```

Striping + parallel reads = 4× more parallelism for 4 stripes.

**Failover within parallel reads:**

```
3 chunks in flight: chunk 0 from Node A, chunk 1 from Node B, chunk 2 from Node C

  chunk 0: Node A → success ✓
  chunk 1: Node B → TIMEOUT ✗  → re-fetch chunk 1 from Node A or C (they have the full blob)
  chunk 2: Node C → success ✓

  Client still gets the complete file — one slow/dead node doesn't block the read.
  Only the failed chunk is re-fetched, not the entire file.
```

#### 1.4 Write Path — DistributedStore.Put()

```
Client writes foo.txt → Node C (any node)

1. Hash content: hash = sha256(content)

2. Data placement via existing CRUSH:
   dataTargets = clusterMap.Placement(hash, dataReplicationFactor)
   → [Node B, Node D, Node E]
   (unchanged — CRUSH already works)

3. Stream blob to data targets (parallel, existing replication):
   ├── Node B: PutBlob(hash, content)  ── existing connectToPeerStream
   ├── Node D: PutBlob(hash, content)  ── existing connectToPeerStream
   └── Node E: PutBlob(hash, content)  ── existing connectToPeerStream
   Wait for quorum (2 of 3 ack) — existing ACK protocol

4. Metadata write to shard owner:
   shardKey = consistentHash("foo.txt", numShards)
   shardOwner = ring.Lookup(shardKey) → Node A

   if Node C == Node A:
     local BoltDB write (existing CASStore.Put metadata path)
   else:
     RPC(Node A, "PutMetadata", {name: "foo.txt", hash, size, replicas: [B,D,E]})

   Node A replicates metadata to M-1 metadata replicas (new: metadata replication)

5. Return success to client
```

**Note**: Step 2-3 uses the *existing* CRUSH + replication infrastructure. The only new piece is step 4: metadata writes go to the shard owner instead of local-only.

#### 1.5 Failover During Read

```
Node C proxies read from replica list [B, D, E]:

  try Node B (lowest RTT):
    ├── success → stream to client ✓
    ├── timeout (5s) → mark B suspect in SWIM, try D
    └── connection reset → mark B suspect, try D immediately

  try Node D:
    ├── success → stream to client ✓
    └── failure → try E

  try Node E:
    ├── success → stream to client ✓
    └── failure → return syscall.EIO

  Partial read failover:
    Client retries with HTTP Range header (offset = bytes already received)
    Momo protocol already supports offset-based streaming
```

This builds on the existing SWIM failure detection — a node marked suspect stops being selected as a proxy target.

#### 1.6 Read Cache Design

```
Two-level LRU cache on every node:

Level 1: Metadata Cache
  ┌────────────────────────────────────────────┐
  │ key: file name (string)                    │
  │ val: *ResolvedMeta (~200 bytes)            │
  │ size: 10,000 entries (configurable)        │
  │ TTL: 60s (configurable)                    │
  │ memory: ~2MB                               │
  │ hit rate: ~95% for hot workloads           │
  └────────────────────────────────────────────┘

Level 2: Blob Cache
  ┌────────────────────────────────────────────┐
  │ key: content hash (string)                 │
  │ val: blob bytes ([]byte)                   │
  │ size: 256MB total (configurable)           │
  │ max entry: 4MB (don't cache large files)   │
  │ eviction: LRU by last access               │
  │ hit rate: depends on access pattern        │
  └────────────────────────────────────────────┘

Config (new [read_cache] section):
  metadata_entries = 10000
  metadata_ttl = "60s"
  blob_memory_mb = 256
  blob_max_size_mb = 4
```

#### 1.7 Consistent Hash Ring (Metadata Sharding)

The existing `ClusterMap.Placement()` uses weighted rendezvous hashing for *data* placement. Metadata sharding uses a separate consistent hash ring — this allows metadata and data to have different replication factors and different placement strategies.

```go
// HashRing maps shard keys to node IDs using consistent hashing.
// When nodes are added/removed, only K/N keys remap (vs. all keys for mod-N).
type HashRing struct {
    virtualNodes int      // virtual nodes per physical node (default 150)
    nodes        []ringNode
}

// Lookup returns the node ID that owns the given shard key.
func (r *HashRing) Lookup(key string) int

// AddNode/RemoveNode update the ring with minimal key remapping.
func (r *HashRing) AddNode(nodeID int, weight int)
func (r *HashRing) RemoveNode(nodeID int)
```

**Why a separate ring**: Data placement (CRUSH) optimizes for disk bandwidth and rack/zone awareness. Metadata placement (hash ring) optimizes for lookup speed and minimal remapping on cluster changes. Different goals, different algorithms.

#### 1.8 Metadata Replication

Each metadata shard is replicated to `metadata_replication` nodes (default 3, separate from data `replication_factor`). Writes go to the shard owner, which synchronously replicates to M-1 peers. Reads can go to any replica (fallback if owner is down).

```
Write "foo.txt" metadata:
  shardOwner = ring.Lookup("foo.txt") → Node A
  metadataReplicas = ring.Replicas("foo.txt", 3) → [A, F, G]

  Node A writes to local BoltDB
  Node A replicates to Node F (sync RPC)
  Node A replicates to Node G (sync RPC)
  Quorum = 2 of 3 → ack after 2 writes succeed

Read "foo.txt" metadata:
  Try Node A (shard owner)
  If A is down → try Node F → try Node G
  (any replica can serve reads — read repair if stale)
```

This reuses the existing lease consensus infrastructure for quorum writes.

---

### Pillar 2: Customer-Transparent Complexity

#### 2.1 What the Customer Sees vs. What Happens Internally

```
Customer sees:                     Internally (invisible to customer):
┌──────────────────────┐          ┌──────────────────────────────────┐
│ S3 endpoint          │          │ Any node receives request        │
│ PUT /foo.txt         │ ───────► │ → hash content (SHA-256)         │
│ GET /foo.txt         │          │ → CRUSH data placement           │
│ DELETE /foo.txt      │          │ → metadata shard resolve         │
│ GET / (list)         │          │ → parallel blob write to targets │
│ HEAD /foo.txt        │          │ → metadata write to shard owner  │
│                      │          │ → quorum ack                     │
│ One filesystem.      │          │ → replication (invisible)        │
│ No cluster awareness.│          │ → caching (invisible)            │
│ No client-side       │          │ → failover (invisible)           │
│ routing.             │          │ → healing (invisible)            │
│ No special SDK.      │          │ → sharding (invisible)           │
└──────────────────────┘          └──────────────────────────────────┘
```

The customer uses standard tools (aws-cli, boto3, rclone, curl) against any node's address. Everything else is internal.

#### 2.2 S3 API → Distributed Operation Mapping

| S3 Operation | HTTP Method | Current Implementation | MomoFS Distributed Operation |
|---|---|---|---|
| PutObject | `PUT /key` | Local store + replication | Hash → CRUSH → parallel blob write → metadata shard write |
| GetObject | `GET /key` | Local store only | Metadata resolve → local hit or proxy from replica |
| DeleteObject | `DELETE /key` | Local tombstone + gossip | Metadata resolve → tombstone on shard owner → P2P gossip → refcount decrement |
| ListObjectsV2 | `GET /?list-type=2` | Scatter-gather all nodes | Scatter-gather metadata shard owners only → merge → paginate |
| HeadObject | `HEAD /key` | Local store only | Metadata resolve → return size/meta (no blob read) |
| CopyObject | `PUT /dst` + `x-amz-copy-source` | Not supported | Metadata resolve src → metadata write dst (no blob copy if same hash) |
| ListObjectsV2 + prefix | `GET /?prefix=photos/` | Scatter-gather all | Only shard owners for that prefix range → fewer RPCs |

**Key improvement**: Current `ListObjectsV2` scatter-gathers to *all* N nodes (O(N) RPCs). MomoFS only queries the shard owners for the requested prefix range — typically O(shard_owners) = O(N/M) where M = shards per node. For a 100-node cluster with 256 shards, that's ~4 RPCs instead of 100.

#### 2.3 FUSE Mount (Optional POSIX Interface)

> **Status**: implemented. The FUSE transport (`momo -imp fs`, bazil.org/fuse binding) ships in `src/momofs` and mounts the daemon's CAS store as a POSIX tree (R4, #962/#963). Operation mapping per section below; follow-ups tracked in openspec/changes/r4-momofs/tasks.md (mmap/byte-range + posix-locks, scrub/GC coexistence). User guide: [MOUNT_USER_GUIDE.md](MOUNT_USER_GUIDE.md).

For HPC and legacy applications that need a POSIX filesystem interface:

```
Mount: momo -imp fs -id 0 -fs-mount /mnt/momo

/mnt/momo/
├── photos/
│   ├── sunset.jpg    → GET /photos/sunset.jpg
│   └── beach.png     → GET /photos/beach.png
├── documents/
│   ├── report.pdf    → GET /documents/report.pdf
│   └── notes.txt     → GET /documents/notes.txt
└── data/
    └── dataset.csv   → GET /data/dataset.csv

FUSE operation → Momo operation:
  open(path)      → HeadObject(path) → size, permissions
  read(path, off) → GetObject(path, Range: bytes=off-) → stream
  write(path)     → PutObject(path, body) → hash + store
  unlink(path)    → DeleteObject(path) → tombstone
  mkdir(path)     → metadata-only op on shard owner
  readdir(path)   → ListObjectsV2(prefix=path/) → directory listing
  rename(a, b)    → CopyObject(a→b) + DeleteObject(a) → atomic on shard owner
  getattr(path)   → HeadObject(path) → stat structure
```

The FUSE daemon runs client-side and talks to any Momo node via the existing S3 API. No cluster-side changes needed — it's a pure client-side adapter.

#### 2.4 Client SDK Design (Zero Cluster Awareness)

```go
// Customer code — nothing about clusters, shards, or replication:
client := momo.NewClient("nodeA:4440")

err := client.Put("photos/sunset.jpg", reader)
data, err := client.Get("photos/sunset.jpg")
files, err := client.List("photos/")
err := client.Delete("photos/sunset.jpg")

// Multi-endpoint (optional, for HA):
client := momo.NewClient("nodeA:4440", "nodeB:4440", "nodeC:4440")
// Client round-robins and fails over transparently
```

The client never computes CRUSH, never knows the replication factor, never knows about metadata sharding. It's just a storage client talking to an endpoint.

---

### Pillar 3: HPC Ready — Implementation

#### 3.1 Striped Parallel Read Protocol

For large files (> `stripe_size`), data is striped across multiple nodes. A read pulls stripes in parallel, maximizing aggregate bandwidth.

```
File: dataset.csv (1TB)
Striped across 4 nodes, stripe_size = 64MB

Node A: stripes 0, 4, 8, 12, ...  (offsets 0, 256MB, 512MB, ...)
Node B: stripes 1, 5, 9, 13, ...  (offsets 64MB, 320MB, 576MB, ...)
Node C: stripes 2, 6, 10, 14, ... (offsets 128MB, 384MB, 640MB, ...)
Node D: stripes 3, 7, 11, 15, ... (offsets 192MB, 448MB, 704MB, ...)

Read request (offset=0, length=1TB):
  ├── goroutine 1: read stripes [0,4,8,...]  from Node A (parallel)
  ├── goroutine 2: read stripes [1,5,9,...]  from Node B (parallel)
  ├── goroutine 3: read stripes [2,6,10,...] from Node C (parallel)
  └── goroutine 4: read stripes [3,7,11,...] from Node D (parallel)

  Merge in stripe order → stream to client

  Aggregate bandwidth = 4 × single_node_bandwidth
  Time = 1TB / (4 × disk_BW)  vs.  1TB / disk_BW  (4× faster)
```

**Implementation**: Striping is metadata-driven. `ResolvedMeta` includes stripe layout:

```go
type ResolvedMeta struct {
    // ... existing fields ...
    Striped bool
    StripeSize int64        // bytes per stripe (0 if not striped)
    StripeNodes []int       // node IDs holding stripes (round-robin)
}
```

For non-striped files (default), `Striped = false` and the existing single-blob read path is used. Striping is enabled per-bucket or per-tenant.

#### 3.2 Concurrent Read Scaling

```
1000 HPC processes read the same 1GB file simultaneously:

Traditional shared filesystem (NFS):
  → All 1000 processes hit the same NFS server
  → NFS server bottleneck: 1 × disk_bandwidth
  → Per-process bandwidth: disk_BW / 1000  (scales terribly)

MomoFS with 3 replicas + blob cache:
  → 1000 processes distributed across cluster nodes
  → Each node serves from local blob or cache
  → With 3 replicas: 3 nodes serve from disk in parallel
  → With blob cache (256MB): hot reads hit memory
  → Per-process bandwidth: min(disk_BW, cache_BW) / local_contention
  → Aggregate: min(N_readers, N_replicas) × disk_BW  (or cache speed)

  Scale: add nodes → more replicas → more parallel read bandwidth
  With 10 replicas: 10 × disk_BW aggregate  (10× NFS throughput)
```

#### 3.3 MPI-IO Adapter

```
MPI-IO calls → MomoFS operations (via S3 API or FUSE mount):

MPI_File_open(path)     → HeadObject(path) or PutObject(path)
MPI_File_read(fh, buf)  → GetObject(path, Range: offset+length)
                           → striped parallel read if large + striped
MPI_File_write(fh, buf) → PutObject(path, reader)
MPI_File_close(fh)      → (no-op, Momo handles persistence)
MPI_File_set_view()     → (client-side offset tracking, no cluster RPC)

Key properties:
  - MPI processes run on any compute node
  - Data accessible from any node (Read From Any)
  - No shared filesystem mount needed
  - No data pinning required
  - Scheduler places jobs anywhere — data finds the compute
```

#### 3.4 Performance Model

```
Read latency:
  Local hit (NVMe):  T = T_bolt_lookup + T_nvme_read
                     = 0.1ms + 0.5ms = 0.6ms

  Local hit (SSD):   T = 0.1ms + 2.0ms = 2.1ms

  Cache hit:         T = T_lru_lookup + T_mem_copy
                     = 0.01ms + 0.01ms = 0.02ms

  Remote hit (same DC): T = T_meta_resolve + T_network + T_disk
                       = 0.5ms + 1.0ms + 0.5ms = 2.0ms

  Remote hit (cross DC): T = 0.5ms + 5.0ms + 2.0ms = 7.5ms

Read bandwidth (striped, single file):
  B = N_stripes × min(disk_BW_per_node, network_BW)
  Example: 4 stripes, 10Gbps network, 7Gbps NVMe per node:
  B = 4 × min(7, 10) = 28 Gbps = 3.5 GB/s

Read bandwidth (concurrent, same file):
  B = min(N_readers, N_replicas) × disk_BW_per_replica
  With cache: B = min(N_readers, N_nodes) × cache_BW

Metadata resolution latency:
  Cache hit:    ~0.01ms (in-memory LRU)
  Local shard:  ~0.1ms  (BoltDB lookup)
  Remote shard: ~0.5ms  (RPC to shard owner + BoltDB)
```

#### 3.5 HPC Configuration

```toml
[striping]
enabled = false          # enable for HPC workloads with large files
stripe_size_mb = 64      # stripe size (match to disk block size for best perf)
stripe_count = 0         # 0 = auto = num_nodes

[read_cache]
metadata_entries = 10000
metadata_ttl = "60s"
blob_memory_mb = 512     # larger for HPC (default 256)
blob_max_size_mb = 16    # larger for HPC (default 4)
```

#### 3.6 Concurrent I/O Architecture

Concurrent I/O is the ability to handle thousands of simultaneous read and write operations without blocking. MomoFS is designed for massive concurrency at every layer.

**Layer 1: Goroutine-per-request (Go runtime scheduler)**

Every incoming request — whether from a client, a peer node, or an internal scrub thread — gets its own goroutine. Go's M:N scheduler multiplexes goroutines onto OS threads efficiently.

```
Node receiving 10,000 concurrent requests:

  ├── goroutine 1:  serve GET /file1.bin  → parallel multi-node read (3 goroutines)
  ├── goroutine 2:  serve GET /file2.bin  → local hit, stream from disk
  ├── goroutine 3:  serve PUT /file3.bin  → CRUSH + parallel replicate (3 goroutines)
  ├── goroutine 4:  serve GET /file4.bin  → parallel multi-node read (3 goroutines)
  ├── goroutine 5:  serve DELETE /file5   → tombstone + gossip
  ├── goroutine 6:  scrub check           → background, doesn't block requests
  ├── ...
  └── goroutine 10000: serve GET /fileN.bin → cache hit, stream from memory

  Total goroutines: ~30,000 (10K requests × ~3 goroutines each for parallel reads)
  Go scheduler: multiplexes onto GOMAXPROCS threads (default: num CPUs)
  Memory per goroutine: ~8KB stack (initial, grows as needed)
  Total goroutine memory: ~240MB (30K × 8KB) — trivial for a server
```

**Layer 2: Connection semaphore (existing, `server.go`)**

The existing semaphore limits concurrent connections to prevent resource exhaustion. This is already implemented in the codebase.

```go
// Existing in server.go — bounded concurrency for incoming connections
sem := make(chan struct{}, maxConnections) // default 1000, configurable

for {
    conn, err := server.Accept()
    sem <- struct{}{} // acquire (blocks if at capacity)
    go func() {
        defer func() { <-sem }() // release
        handleConnection(conn)
    }()
}
```

MomoFS extends this with per-operation semaphores:

```
Connection semaphore:     10,000 concurrent client connections (configurable)
Parallel read semaphore:  5,000 concurrent inter-node range fetches (for parallel multi-node reads)
Scrub semaphore:          10 concurrent scrub operations (background, low priority)

Total goroutines at peak: ~30,000 (10K connections × 3 parallel reads)
But disk I/O is bounded by the OS — goroutines block on I/O, yielding CPU to others.
```

**Layer 3: BoltDB concurrency (RWMutex)**

BoltDB supports concurrent readers with a single writer. `CASStore` already uses `sync.RWMutex`:

```
Reads:  unlimited concurrent readers (RLock)
        ├── 100 goroutines reading different keys → all proceed in parallel
        ├── 100 goroutines reading the SAME key → all proceed in parallel
        └── BoltDB uses MVCC — readers never block readers

Writes: single writer (Lock), but only per-shard
        ├── Write to shard A → locks shard A only
        ├── Write to shard B → locks shard B only (parallel with A)
        └── Different shards never block each other

Mixed: reads don't block writes to different keys
        ├── Read "foo.txt" (RLock) → proceeds
        ├── Write "bar.txt" (Lock on different shard) → proceeds in parallel
        └── Only read-of-being-written-key waits (and only briefly)
```

**Layer 4: Inter-node connection pooling**

Parallel multi-node reads (section 1.3a) open connections to replica nodes. These are pooled to avoid TCP/QUIC handshake overhead.

```go
type ConnectionPool struct {
    pools map[int]*sync.Pool  // nodeID → connection pool
}

// Get returns a pooled connection or creates a new one.
// Connections are reused across parallel read goroutines.
func (p *ConnectionPool) Get(nodeID int) (net.Conn, error)
func (p *ConnectionPool) Put(nodeID int, conn net.Conn)
```

```
Without pooling:
  Parallel read of 3 chunks → 3 new connections → 3 × TCP handshake (~1ms each)
  Repeated reads → repeated handshakes → wasted latency

With pooling:
  First parallel read → 3 new connections (handshake)
  Subsequent reads → reuse pooled connections (0ms handshake)
  Pool per node: max 10 idle connections per peer (configurable)
  QUIC: multiplexed streams — no handshake at all after initial connection
```

**Layer 5: Backpressure and flow control**

When the system is overloaded, it degrades gracefully rather than collapsing:

```
Overload scenario: 10,000 clients all read large files simultaneously

  1. Connection semaphore (10K limit):
     → Connections beyond 10K queue (client sees slight latency increase)
     → No OOM from unbounded goroutine creation

  2. Parallel read semaphore (5K limit):
     → At most 5K concurrent inter-node range fetches
     → Excess goroutines wait — fair scheduling via Go scheduler

  3. BoltDB: readers never block → metadata lookups stay fast even under load

  4. Disk I/O: OS I/O scheduler handles queueing → disk throughput stays at max
     → Goroutines blocked on I/O yield CPU → other goroutines proceed

  5. Network: TCP/QUIC flow control → sender backs off if receiver is slow
     → No buffer overflow, no packet loss from flooding

  6. Polymorphic system (existing):
     → CPU/memory threshold exceeded → switch to lower-overhead replication mode
     → Reduces per-write cost → more capacity for serving reads

  Result: latency increases gradually (graceful degradation), no crashes, no OOM
```

**Concurrency model summary:**

```
┌──────────────────────────────────────────────────────────────────┐
│                    Concurrent I/O Stack                           │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Client requests:  10,000 concurrent (connection semaphore)      │
│       │                                                          │
│       ▼                                                          │
│  Goroutines:       ~30,000 (1 per request + parallel read children) │
│       │                                                          │
│       ├──► BoltDB reads:    unlimited concurrent (RWMutex RLock) │
│       │    ├── Different keys: fully parallel                    │
│       │    └── Same key:     fully parallel (MVCC)              │
│       │                                                          │
│       ├──► BoltDB writes:   serialized per-shard (RWMutex Lock) │
│       │    ├── Different shards: parallel                        │
│       │    └── Same shard:   serialized (brief)                 │
│       │                                                          │
│       ├──► Inter-node reads: 5,000 concurrent (read semaphore)  │
│       │    ├── Connection pool: reuse across goroutines          │
│       │    └── QUIC: multiplexed streams, no handshake           │
│       │                                                          │
│       └──► Background scrub: 10 concurrent (low priority)       │
│            └── Never blocks foreground requests                  │
│                                                                  │
│  Go scheduler: M:N multiplexing onto GOMAXPROCS threads         │
│  Goroutines blocked on I/O yield CPU → high utilization         │
│  Memory: ~8KB per goroutine → 30K goroutines = ~240MB (trivial) │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

**Concurrent I/O performance targets:**

| Metric | Target | How |
|--------|--------|-----|
| Concurrent client connections | 10,000+ | Connection semaphore (configurable, existing) |
| Concurrent inter-node reads | 5,000+ | Parallel read semaphore (new) |
| Concurrent BoltDB readers | Unlimited | RWMutex RLock (existing, MVCC) |
| Concurrent shard writers | N_shards | One writer per shard, parallel across shards |
| Goroutines at peak | ~30,000 | Go scheduler handles efficiently (~240MB memory) |
| Read latency under load | < 2× unloaded | Parallel reads + cache + connection pooling |
| Write latency under load | < 3× unloaded | Quorum writes, brief per-shard lock |
| Graceful degradation | Latency rises, no crash | Semaphores + backpressure + polymorphic system |

**Why this works**: Go's goroutine model makes massive concurrency cheap. BoltDB's MVCC means readers never block. The per-shard write lock means writes to different keys proceed in parallel. Connection pooling avoids handshake overhead. The existing semaphore + polymorphic system provide backpressure. Every layer is designed for concurrency.

---

### Pillar 4: Cloud Ready — Implementation

#### 4.1 Kubernetes Deployment (StatefulSet)

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: momo
spec:
  serviceName: momo-headless
  replicas: 5
  podManagementPolicy: Parallel
  template:
    spec:
      containers:
      - name: momo
        image: momo:latest
        ports:
        - containerPort: 4440   # data plane (TCP + QUIC + S3)
        - containerPort: 4450   # P2P gossip
        - containerPort: 9100   # Prometheus metrics
        env:
        - name: MOMO_NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name   # momo-0, momo-1, ...
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 1Ti
---
apiVersion: v1
kind: Service
metadata:
  name: momo-headless
spec:
  clusterIP: None        # headless → each pod gets DNS name
  selector:
    app: momo
  ports:
  - port: 4440
---
apiVersion: v1
kind: Service
metadata:
  name: momo             # round-robin → any node can serve any request
spec:
  type: LoadBalancer
  selector:
    app: momo
  ports:
  - port: 4440
```

**Why this works with MomoFS**:
- **StatefulSet**: stable pod names (momo-0, momo-1) → stable node IDs for P2P
- **Headless service**: each pod gets a DNS name → P2P discovery via DNS
- **PVC per pod**: persistent local storage → BoltDB + blobs survive restarts
- **LoadBalancer service**: round-robin → any node serves any request (Read From Any)
- **No leader pod**: no special StatefulSet ordinal requirements
- **Parallel management**: all nodes start simultaneously

#### 4.2 CSI Driver (Container-Native Volumes)

```yaml
# Pod mounts MomoFS as a volume — no S3 client needed in the pod
apiVersion: v1
kind: Pod
spec:
  volumes:
  - name: momo-vol
    csi:
      driver: momo.csi.io
      volumeAttributes:
        endpoint: momo-headless:4440
        bucket: my-bucket
  containers:
  - name: app
    volumeMounts:
    - name: momo-vol
      mountPath: /mnt/data
```

CSI operations:
- `CreateVolume` → no-op (Momo has no pre-allocation)
- `DeleteVolume` → delete all objects with prefix (optional)
- `NodeStage` → FUSE mount MomoFS at staging path
- `NodePublish` → bind-mount staging path to container path
- `NodeUnpublish` → unmount

The CSI driver is a thin wrapper around the FUSE mount (Pillar 2.3).

#### 4.3 Auto-Scaling

```
Scale up (add nodes):
  1. New pod starts (StatefulSet scale)
  2. New node joins P2P gossip cluster (existing SWIM join)
  3. Cluster map updated → all nodes notified (existing gossip)
  4. Hash ring updated → new node gets metadata shard assignments
  5. Background: data rebalances to new node (CRUSH reweight)
  6. New node serves reads immediately (proxies if no local data yet)

Scale down (remove nodes):
  1. Pod terminates → node leaves cluster (graceful via SIGTERM + ctx.Done)
  2. Cluster map updated → CRUSH placement recalculated
  3. Under-replicated objects detected by scrub (Phase 2)
  4. Background: re-replicate to maintain replication factor
  5. No downtime — other nodes serve reads during evacuation

Triggers (K8s HPA or custom operator):
  - CPU/mem: existing polymorphic system already monitors these
  - Connections: momo_active_connections > threshold
  - Storage: momo_disk_free_bytes < threshold
  - All metrics already exported via Prometheus
```

#### 4.4 Multi-Region

```
Region A (us-east)              Region B (eu-west)
┌──────────────────┐           ┌──────────────────┐
│ Momo Cluster A   │           │ Momo Cluster B   │
│ 5 nodes          │◄─────────►│ 5 nodes          │
│ Local storage    │  cross-   │ Local storage    │
│                  │  region   │                  │
│ S3: a.momo.io    │  repl     │ S3: b.momo.io    │
└──────────────────┘           └──────────────────┘

Cross-region replication:
  - Async (eventual consistency, RPO configurable per tenant)
  - Per-tenant: tenant opts in/out of geo-replication
  - Data residency: tenant data pinned to region (GDPR Article 44)
  - Failover: DNS switch a.momo.io → b.momo.io (RTO < 60s)
  - Protocol: P2P gossip extended cross-cluster (low-frequency heartbeats)
```

#### 4.5 Billing & Metering

```
Per-tenant metrics (sync/atomic counters, zero per-request overhead):
  momo_tenant_bytes_used{tenant}       → $/GB/month
  momo_tenant_bytes_transferred{tenant} → $/GB
  momo_tenant_requests_total{tenant}   → $/1000-requests

Pipeline:
  Momo atomic counters → Prometheus scrape → recording rules →
  → per-tenant /metrics → billing system

  No per-request billing overhead — counters are sync/atomic (~5ns).
  Aggregation happens at scrape time (every 15-60s).
```

---

### Configuration Design

New config sections added to `Configuration` in `src/common/struct.go`:

```go
type Configuration struct {
    // ... existing fields ...
    MomoFS     ConfigurationMomoFS     // new: distributed filesystem
    ReadCache  ConfigurationReadCache  // new: read caching
    Striping   ConfigurationStriping   // new: HPC striped reads
}

type ConfigurationMomoFS struct {
    Enabled              bool   // enable distributed metadata (Phase 1)
    MetadataShards       int    // total virtual shards (default 256)
    MetadataReplication  int    // metadata replica count (default 3)
    MetadataQuorum       int    // writes need quorum ack (default 2)
}

type ConfigurationReadCache struct {
    MetadataEntries int    // LRU entries for metadata (default 10000)
    MetadataTTL     int    // TTL in seconds (default 60)
    BlobMemoryMB    int    // max memory for blob cache (default 256)
    BlobMaxSizeMB   int    // don't cache blobs larger than this (default 4)
}

type ConfigurationStriping struct {
    Enabled      bool   // enable striped reads
    StripeSizeMB int    // stripe size (default 64)
    StripeCount  int    // stripes per file (0 = auto = num_nodes)
}
```

TOML config file:

```toml
[momofs]
enabled = false               # backward compat: false = current behavior
metadata_shards = 256
metadata_replication = 3
metadata_quorum = 2

[read_cache]
metadata_entries = 10000
metadata_ttl = 60             # seconds
blob_memory_mb = 256
blob_max_size_mb = 4

[striping]
enabled = false
stripe_size_mb = 64
stripe_count = 0              # 0 = auto
```

**Backward compatibility**: When `momofs.enabled = false` (default), the system uses the existing `CASStore` with local-only metadata. No behavior change. When `enabled = true`, the `DistributedStore` wrapper activates and metadata becomes sharded across the cluster.

---

### Phase 1 Task Breakdown — Distributed Metadata (Grounded in Existing Code)

Phase 1 is the foundation: it enables "Read From Any Node" by making metadata distributed. Every task references existing code that it extends or wraps.

| Task | Files Changed | Description |
|------|---------------|-------------|
| 1. HashRing | `src/common/hash_ring.go` (new) | Consistent hash ring for metadata shard ownership. Separate from CRUSH (which is for data). `Lookup(key) → nodeID`. |
| 2. MetadataResolver | `src/storage/metadata_resolver.go` (new) | `Resolve(ctx, name) → *ResolvedMeta`. If this node owns the shard, reads from local `CASStore` BoltDB. Otherwise, RPCs shard owner via existing P2P transport. |
| 3. MetadataResolve RPC | `src/p2p/scatter_gather.go` (extend) | New `QueryType: QueryResolveMetadata`. `StorageQueryHandler.HandleQuery` gains a case that calls `store.Get(name)` and returns metadata without blob data. |
| 4. BlobProxy | `src/storage/blob_proxy.go` (new) | `FetchBlob(ctx, hash, replicas) → io.ReadCloser`. Connects to replica node via existing `client.Connect`, streams blob. Tries replicas in RTT order (RTT from P2P peer map). Failover on timeout. |
| 5. ReadCache | `src/storage/read_cache.go` (new) | Two-level LRU: metadata cache (string→ResolvedMeta, TTL) + blob cache (string→[]byte, memory-bounded). Uses `sync.Map` for metadata, `container/list` for LRU blob. |
| 6. DistributedStore | `src/storage/distributed_store.go` (new) | Implements existing `Store` interface. Composes `CASStore` + `MetadataResolver` + `BlobProxy` + `ReadCache`. `Get()` does resolve→local-check→proxy. `Put()` does CRUSH→replicate→metadata-write. |
| 7. Metadata replication | `src/storage/metadata_resolver.go` (extend) | Shard owner writes metadata to local BoltDB + sync RPCs to M-1 metadata replicas. Quorum ack. Reuses existing lease consensus for quorum. |
| 8. Config parsing | `src/common/struct.go`, `src/common/config.go` | Add `ConfigurationMomoFS`, `ConfigurationReadCache`, `ConfigurationStriping` structs. Parse `[momofs]`, `[read_cache]`, `[striping]` sections. |
| 9. Store factory | `src/storage/factory.go` (extend) | `NewStore()` checks `cfg.MomoFS.Enabled`. If true, wraps `CASStore` in `DistributedStore`. If false, returns `CASStore` as before. |
| 10. Wire hash ring | `src/server/server.go` (extend) | On startup, build `HashRing` from cluster map. Pass to `DistributedStore`. Update ring on cluster membership changes (existing gossip callback). |
| 11. Vector clocks | `src/storage/vector_clock.go` (new) | Per-metadata vector clock for conflict resolution. `Compare() → Before/After/Concurrent`. Stored in `objects` bucket (extend `ObjectMeta` or new field). |
| 12. Integration tests | `src/storage/distributed_test.go` (new) | Multi-node test: write to node A, read from node B (not a replica). Verify data integrity. Test failover: kill shard owner, read from replica. |

**What doesn't change**: `server.Daemon`, `client.Client`, `transport.*`, `BlobStore` interface, existing `CASStore` (still used locally within `DistributedStore`). The server daemon calls `store.Get()` — it doesn't know or care whether the store is local or distributed.

