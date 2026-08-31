# Distributed Metadata Replication

## Change ID
distributed-metadata-v1

## Status
Ratched

## Summary
Enable full "Read From Any Node" capability by replicating metadata shards across multiple nodes via consistent hashing + quorum protocol. Eliminate scatter-gather to all N nodes for metadata resolution.

## Motivation
Current MomoFS stores metadata (namespace→hash, object metadata) locally-only per node. If client writes file to node A, node B has no knowledge unless it's a data replica. ListOperations scatters to all N nodes (O(N) RPCs). This change adds distributed metadata replication to enable:
- Any node can serve any read request (customer-transparent)
- ListObjectsV2 only queries shard owners (O(M) not O(N))
- Automatic failover if shard owner is down
- Foundation for Phase 2 self-healing features

## Owner
@alsotoes

## Dependencies
- Consistent hash ring already exists (Phase 1, item 1)
- BoltDB ObjectMeta extension (checksums, pinning from lessons learned)
- P2P gossip infrastructure (already exists)
- RPC framework (existing scatter-gather pattern)

## Caveats
- Metadata replication separate from data replication (configurable factors)
- Quorum protocol adds ~0.5ms latency for writes across shard replicas
- Vector clocks needed for concurrent write conflict resolution
- WAL recovery not included in this change (Phase 2)

## Technical Details

### Shard Ownership
- Consistent hash ring: 256 default shards, 150 vnodes per physical node
- `ring.Lookup(key)` → node ID owning metadata shard for that key
- `ring.Replicas(key, M)` → M node IDs (default M=3) that have replicas

### Write Path (distributed metadata)
```
1. Client → Node C: PUT /file.txt
2. Node C: dataTargets = CRUSH.Placement(hash, RF) → [N7, N42, N88]  (existing)
3. Node C: stream blob to data targets → quorum ack  (existing)
4. Node C: shardKey = consistentHash("file.txt", 256)  (existing hash)
5. Node C: shardOwner = ring.Lookup(shardKey) → Node A
6. Node C: metadataReplicas = ring.Replicas(shardKey, 3) → [Node A, Node F, Node G]
7. Node C → Node A: RPC("PutMetadata", {name: "file.txt", hash, size, replicas: [N7,N42,N88]})
8. Node A: write to local BoltDB + replicate to F, G (sync RPC)
9. Node A: wait for quorum (2 of 3 acks) → return success to Node C
10. Node C: return "200 OK" to client
```

### Read Path (distributed metadata)
```
1. Client → Node X: GET /file.txt
2. Node X: check metadata cache (TTL=60s) → HIT? → serve from cache
3. Node X: shardKey = consistentHash("file.txt", 256)
4. Node X: shardOwner = ring.Lookup(shardKey) → Node A
5. Node X: if X == Node A: meta = local BoltDB (<0.1ms)
   else: RPC(Node A, "ResolveMetadata", "file.txt")
6. Node A: look up local BoltDB → {hash, size, replicas: [N7,N42,N88]}
7. Node A: return meta to Node X
8. Node X: cache metadata (TTL=60s)
9. Node X: check local blob → exist? → serve directly / proxy from best replica
```

### RPC Methods

#### PutMetadata
```go
type PutMetadataArgs struct {
    Name      string
    Hash      string
    Size      int64
    Replicas  []int
    VClock    []uint64
    Checksum  uint32
    ShardKey  string
}

type PutMetadataReply struct {
    Success     bool
    ExistingVClock []uint64
}
```

#### ReplicateMetadata
```go
type ReplicateMetadataArgs struct {
    ShardKey  string
    ObjectMeta ObjectMeta
}

type ReplicateMetadataReply struct {
    Success bool
}
```

#### ResolveMetadata
```go
type ResolveMeta struct {
    Hash       string
    Size       int64
    Replicas   []int
    DeletedAt  time.Time
    VectorClock []uint64
    RemotePath string
}
```

### Configuration

```toml
[momofs]
metadata_replication = 3        # separate from replication_factor = 3
metadata_quorum = 2             # (metadata_replication/2)+1 = 2
metadata_ttl = "60s"            # cache TTL
```

### BoltDB Schema Changes

Add to `ObjectMeta` struct:
```go
type ObjectMeta struct {
    Size       int64
    RefCount   int64
    DeletedAt  int64
    Checksum   uint32      // CRC32C of blob content
    Replicas   []int       // data node IDs from CRUSH
    MetadataReplicas []int  // metadata shard replicas from hash ring
    VectorClock []uint64   // for conflict resolution
    ShardKey   string      // consistent hash ring shard key
}
```

Add bucket `metadata_replicas` per node (coordinated via RPC, not shared storage).

### Consistency Model
- **Write quorum**: (metadata_replication/2)+1 replicas must ack
- **Read**: any replica (ONE), with read-repair for stale data
- **Concurrent writes**: vector clocks → last-writer-wins by timestamp
- **Read repair**: if metadata differs across replicas, propagate winning version

### Failure Scenarios

#### Shard Owner Down During Write
```
Write "foo.txt" → metadata replicas [A, B, G]
  Node A: write ✓ (shard owner)
  Node B: write ✓ (quorum met)
  Node G: DOWN → store hint on Node A (Phase 2)
  
  Later, Node G recovers:
  Node A replays hint to Node G → now has metadata
  Hint deleted from Node A
```

#### Shard Owner Down During Read
```
Client → Node X: GET /file.txt
  shardOwner = ring.Lookup(key) → Node 23 (DOWN)

→ Fall back to metadata replicas: [Node 67, Node 91]
→ RPC(Node 67, "ResolveMetadata") → success
→ Node 67 returns same metadata (replica of same shard)

→ SWIM marks Node 23 as SUSPECT
→ Scrub will re-replicate metadata to maintain 3 copies
```

#### Concurrent Writes (Vector Clock Conflict)
```
Node A: PutMetadata("foo.txt", vclock=[A:1]) → quorum [A,B]
Node B: PutMetadata("foo.txt", vclock=[B:1]) → quorum [B,G]

Both succeed (different quorums). On read:
  Node A returns [A:1], Node B returns [B:1]
  → Concurrent clocks → last-writer-wins by timestamp
  → Scrub logs conflict for review
  → Read repair propagates winning version to all replicas
```

### Rationale for Key Design Decisions

1. **Separate replication factor** (metadata_replication vs replication_factor):
   - Metadata changes more frequently than data blobs
   - Metadata is smaller → faster replication
   - Different durability/rpo requirements
   - Allows RF3 for data, RF3 for metadata (or different values per deployment)

2. **Quorum = (M/2)+1** (not M):
   - Allows minority partition to continue operating
   - Standard distributed systems pattern (Paxos/Raft lite)
   - Meets CAP tradeoff: availability over strong consistency

3. **Cache TTL = 60s**:
   - Balances freshness with RPC reduction
   - Hot files become local after 2 reads
   - Matches existing gossip/heartbeat intervals

4. **Vector clocks** (not last-write-wins by timestamp alone):
   - Captures partial ordering of concurrent writes
   - More nuanced conflict resolution
   - Scrub can detect and log conflicts for manual review
   - Preserves intent (e.g., two users editing same doc simultaneously)

5. **No external database** (same philosophy as Ceph):
   - Embedded BoltDB on each node → zero additional infrastructure
   - Cluster itself forms the distributed store
   - MON-like process not needed for data operations
   - MON daemons handle cluster map, not metadata reads/writes

### Test Plan

1. **Unit tests**: Mock shard owner/replica RPCs, verify quorum behavior (2/3 acks)
2. **Integration tests**: 3-node cluster, write file, verify readable from any node
3. **Failure scenarios**: Owner down → fallback to replica → verify read works
4. **Concurrency**: Simultaneous writes to same file → vector conflict detection
5. **ListObjectsV2**: Verify O(shard_owners) RPCs not O(all_nodes)

### Acceptance Criteria
- [ ] Write to any node → metadata replicated to M-1 peers via RPC
- [ ] Read from any node → metadata resolved via shard owner + replicas
- [ ] ListObjectsV2 → only queries shard owners (not all N nodes)
- [ ] Owner down → automatic fallback to replicas
- [ ] Concurrent writes → vector conflict detected + read repair
- [ ] Configuration: metadata_replication independent of data replication
- [ ] Backward compatible: `momofs.enabled = false` → local-only mode unchanged

### Open Questions / Open Issues
1. Hinted handoff for downed replicas (Phase 2 dependency?)
2. WAL integration for crash recovery (Phase 2 roadmap item 6)
3. Per-tenant metadata replication (Phase 4 GDPR feature)
4. Erasure coding compatibility (Phase 7, different placement needs)
5. FUSE mount interaction with distributed metadata (already implemented locally)

### Linked GitHub Issues
- Issue #XXX: Distributed metadata replication design
- Issue #YYY: Vector clocks for metadata conflict resolution
- Issue #ZZW: ListObjectsV2 performance improvement (scatter-gather optimization)