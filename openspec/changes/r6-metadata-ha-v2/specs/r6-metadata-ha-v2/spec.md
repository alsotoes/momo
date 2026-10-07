# R6 Metadata HA v2 — Distributed Metadata Catalog (Any-Node Quorum)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1142

## Purpose

Enable full "Read From Any Node" capability for metadata by replicating metadata across multiple nodes via **any-replica quorum writes** and **any-node reads**. Eliminate the shard-owner coordinator pattern; any node in the replica set can accept writes and serve reads.

## Requirements

### Requirement 1: Hash Ring for Routing Hints (Not Ownership)

#### Scenario: Client routes metadata operation
**Given** a 256-shard consistent hash ring with 150 vnodes/node
**When** client (or any node) needs to perform a metadata operation for key "file.txt"
**Then** the ring provides `Replicas(key, M)` returning M distinct node IDs
**And** the operation is sent concurrently to ALL M replicas
**And** no node is designated "owner" — all M are equal peers for this key

### Requirement 2: Any-Replica Quorum Writes

#### Scenario: Metadata write with quorum
**Given** `metadata_replication=3`, `metadata_quorum=2`
**When** a metadata write occurs (PutMetadata RPC)
**Then** the write is sent concurrently to all 3 replicas
**And** the write succeeds when any 2 replicas acknowledge
**And** there is no designated coordinator — each replica writes locally and acks independently

### Requirement 3: Vector Clock Conflict Detection

#### Scenario: Concurrent writes to different replicas
**Given** two clients write to the same key simultaneously
**When** Client A writes to Replica 1, Client B writes to Replica 2
**And** both achieve quorum (different replica sets)
**Then** each replica records its local vector clock entry
**On read**: differing vector clocks are detected
**And** last-writer-wins by timestamp resolves the conflict
**And** read repair propagates winning version to all replicas

### Requirement 4: Any-Node Reads with Cache

#### Scenario: Metadata read from any node
**Given** a metadata cache (TTL=60s) on each node
**When** a node receives a read request (ResolveMetadata RPC)
**Then** it first checks local cache → HIT: serve immediately (<0.1ms)
**On cache miss**: node sends ResolveMetadata to ALL replicas in parallel
**And** returns first successful response
**And** caches the result (TTL=60s)

### Requirement 5: Read Repair

#### Scenario: Stale replica detected on read
**Given** a read queries all M replicas and gets differing metadata
**When** vector clocks differ or checksums mismatch
**Then** winning version (by timestamp + vector clock) is identified
**And** winning version is propagated to stale replicas via ReplicateMetadata RPC
**And** read returns the winning version to caller

### Requirement 6: Hinted Handoff

#### Scenario: Replica down during write
**Given** a write achieves quorum but one replica is down
**When** the write succeeds on the available replicas
**Then** the successful replicas store a hint (in-memory + persisted) for the downed replica
**When** the downed replica recovers (SWIM marks ALIVE)
**Then** any peer with hints replays them to the recovered replica
**And** hints are deleted after successful replay

### Requirement 7: Shard-Aware ListObjectsV2

#### Scenario: S3 ListObjectsV2 with prefix
**Given** a prefix "tenant/photos/"
**When** ListObjectsV2 is invoked
**Then** the handler determines which shards overlap the prefix
**And** queries only the shard owners (via ResolveMetadata or ListShard RPC)
**And** merges results and returns to client
**And** total RPCs = O(shard_owners) not O(all_nodes)

### Requirement 8: Backup/Restore CLI

#### Scenario: Manual backup
**Given** a running momo node
**When** `momo backup --output /backups --compress` is run
**Then** a consistent bbolt page stream is written to the output
**And** the backup includes metadata: timestamp, node ID, DB version
**And** optional gzip compression is applied

#### Scenario: Restore from backup
**Given** a backup file and a stopped momo node
**When** `momo restore --input /backups/momo-20260101.bak --force` is run
**Then** the backup is validated (header + checksum)
**And** pages are restored to a new DB file
**And** `--force` is required to overwrite existing DB

### Requirement 9: Automated Snapshots

#### Scenario: Periodic backup
**Given** `[global] metadata_snapshot_interval = "24h"`
**When** the interval elapses
**Then** a background goroutine creates a snapshot
**And** writes to configured directory with rotation (daily/weekly)
**And** retention policy: 7 daily + 4 weekly (`metadata_backup_retention`)

## Configuration

```toml
[momofs]
enabled = true
metadata_replication = 3        # separate from replication_factor
metadata_quorum = 2             # (metadata_replication/2)+1
metadata_ttl = "60s"            # cache TTL

[global]
metadata_snapshot_interval = "24h"
metadata_backup_retention = "7d+4w"
```

## BoltDB Schema Changes

Add to `ObjectMeta` struct in `src/storage/storage.go`:

```go
type ObjectMeta struct {
    Size             int64
    RefCount         int64
    DeletedAt        int64
    Checksum         uint32       // CRC32C of blob content
    Replicas         []int        // data node IDs from CRUSH
    MetadataReplicas []int        // metadata shard replica node IDs
    VectorClock      []uint64     // for conflict resolution
    ShardKey         string       // consistent hash ring shard key
}
```

New bucket: `metadata_hints` for hinted handoff (key: shardKey, value: list of pending operations)

## RPC Methods

### PutMetadata (Any-Replica Quorum)
```go
type PutMetadataArgs struct {
    Name           string
    Hash           string
    Size           int64
    Replicas       []int          // Data replicas from CRUSH
    VClock         []uint64       // Client's vector clock
    ShardKey       string         // For hinted handoff
    MetadataReplicas []int        // All M replica node IDs
}

type PutMetadataReply struct {
    Success        bool
    VClock         []uint64       // Updated vector clock
    AckFrom        []int          // Node IDs that acked
}
```

### ResolveMetadata (Any-Node Read)
```go
type ResolveMetadataArgs struct {
    Name           string
    ShardKey       string
}

type ResolveMetadataReply struct {
    Hash           string
    Size           int64
    Replicas       []int
    DeletedAt      int64
    VectorClock    []uint64
    RemotePath     string
    ModTime        int64
    S3Meta         map[string]string
}
```

### ReplicateMetadata (Repair + Hinted Handoff)
```go
type ReplicateMetadataArgs struct {
    ShardKey       string
    ObjectMeta     ObjectMeta
    SourceNode     int            // For hinted handoff tracking
}

type ReplicateMetadataReply struct {
    Success        bool
}
```

## Consistency Model

- **Write quorum**: W = (M/2)+1 replicas must ack (any M replicas)
- **Read**: Query all M replicas in parallel; return first success; cache locally
- **Read repair**: On version mismatch, propagate winning version to all replicas
- **Concurrent writes**: Vector clocks → last-writer-wins by timestamp; scrub logs conflicts

## Failure Scenarios

### Replica Down During Write
```
Write "foo.txt" → replicas [A, B, G] (M=3, W=2)
  Node A: write ✓
  Node B: write ✓ (quorum met)
  Node G: DOWN → A and B store hint for G

Later, Node G recovers:
  A replays hint to G → now has metadata
  Hint deleted from A
```

### Replica Down During Read
```
Client → Node X: ResolveMetadata("file.txt")
  Replicas = [A, B, G]
  G: DOWN (SWIM SUSPECT)
  → Parallel query to A and B
  → First response returned
  → Cache populated on X
```

### Concurrent Writes (Vector Clock Conflict)
```
Node A: PutMetadata("foo.txt", vclock=[A:1]) → quorum [A,B]
Node B: PutMetadata("foo.txt", vclock=[B:1]) → quorum [B,G]

Both succeed. On read:
  A returns [A:1], B returns [B:1]
  → Concurrent clocks → LWW by timestamp
  → Read repair propagates winner to all replicas
  → Scrub logs conflict for review
```

## Acceptance Criteria

- [ ] Write to any node → metadata replicated to M-1 peers via concurrent RPC
- [ ] Write succeeds when any W replicas ack (no coordinator)
- [ ] Read from any node → metadata resolved via cache or parallel replica query
- [ ] ListObjectsV2 → only queries shard owners for prefix (not all N nodes)
- [ ] Replica down → automatic hinted handoff + recovery replay
- [ ] Concurrent writes → vector conflict detected + read repair
- [ ] Configuration: metadata_replication independent of data replication
- [ ] Backward compatible: `momofs.enabled = false` → local-only mode unchanged
- [ ] `momo backup/restore` CLI works; automated snapshots run
- [ ] Integration tests: 3-node cluster, chaos tests for failure scenarios