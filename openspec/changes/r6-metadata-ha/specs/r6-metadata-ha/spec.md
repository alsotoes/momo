> GitHub Issue URL: https://github.com/alsotoes/momo/issues/934

# r6-metadata-ha Specification

## Purpose

Establish a distributed metadata catalog with high availability (quorum replication, automatic failover) and a documented backup/recovery path, enabling "Read From Any Node" capability and O(shard_owners) ListObjectsV2 scalability.

## ADDED Requirements

### Requirement: Consistent Hash Ring for Metadata Sharding

The system SHALL provide a 256-shard consistent hash ring with 150 virtual nodes per physical node to deterministically assign metadata shard ownership.

#### Scenario: Shard ownership lookup
- **GIVEN** a file name "file.txt"
- **WHEN** `ring.Lookup("file.txt")` is called
- **THEN** it returns the node ID owning the shard for that key
- **AND** `ring.Replicas("file.txt", 3)` returns 3 distinct node IDs holding replicas

#### Scenario: Ring stability
- **GIVEN** a cluster of N nodes with stable membership
- **WHEN** `ring.Lookup(key)` is called multiple times
- **THEN** it returns the same owner for the same key
- **AND** adding/removing nodes only affects O(1/M) keys (consistent hashing property)

### Requirement: Metadata RPC Framework

The system SHALL provide a p2p RPC framework for metadata operations with request/response semantics, timeout handling, and panic recovery.

#### RPC Methods
- **PutMetadata** — write metadata to shard owner (quorum W acks)
- **ResolveMetadata** — read metadata from shard owner or replica (R=1)
- **ReplicateMetadata** — replicate metadata from owner to replica

#### Scenario: PutMetadata success
- **GIVEN** a client PUT request routed to Node C
- **WHEN** Node C calls `PutMetadata` on shard owner Node A with M=3 replicas
- **THEN** Node A writes to local BoltDB + replicates to M-1 peers
- **THEN** Node A waits for W=2 acks (including self) → returns success

#### Scenario: ResolveMetadata from any node
- **GIVEN** a client GET request to Node X for "file.txt"
- **WHEN** Node X calls `ResolveMetadata` on shard owner Node A
- **THEN** Node A returns metadata from local BoltDB
- **THEN** Node X caches metadata (TTL=60s) for subsequent requests

### Requirement: Quorum Protocol with Vector Clocks

The system SHALL implement a quorum write protocol with W=(M/2)+1 and vector clock conflict detection.

#### Scenario: Concurrent write conflict detection
- **GIVEN** two clients concurrently write to "file.txt" via different nodes
- **WHEN** Node A's write gets quorum [A,B] with vclock=[A:1]
- **AND** Node B's write gets quorum [B,G] with vclock=[B:1]
- **THEN** both writes succeed (different quorums)
- **THEN** subsequent read detects concurrent vector clocks → logs conflict for scrub review
- **THEN** read repair propagates winning version to all replicas

#### Scenario: Quorum write with replica down
- **GIVEN** shard replicas [A, B, G], M=3, W=2
- **WHEN** Node G is down during write
- **THEN** Node A (owner) writes locally + replicates to B → quorum met (A,B)
- **THEN** hint stored on Node A for G (Phase 3 hinted handoff)
- **WHEN** Node G recovers
- **THEN** Node A replays hint to G → hint deleted

### Requirement: BoltDB Schema Extension

The `ObjectMeta` struct SHALL be extended with distributed metadata fields.

#### Required Fields
```go
type ObjectMeta struct {
    Size              int64
    RefCount          int64
    DeletedAt         int64
    Checksum          uint32
    Replicas          []int          // data node IDs from CRUSH
    MetadataReplicas  []int          // metadata shard replica node IDs
    VectorClock       []uint64       // for conflict resolution
    ShardKey          string         // consistent hash ring shard key
}
```

#### Scenario: Metadata write includes distributed fields
- **GIVEN** a Put operation with content hash H, size S, data replicas D
- **WHEN** shard owner writes ObjectMeta
- **THEN** ObjectMeta.ShardKey = ring.Lookup(name)
- **THEN** ObjectMeta.MetadataReplicas = ring.Replicas(ShardKey, M)
- **THEN** ObjectMeta.VectorClock incremented for this node

### Requirement: Read Path with Cache + Repair

The system SHALL provide a read path with local metadata cache (TTL=60s) and automatic read repair.

#### Scenario: Cache hit after 2 reads
- **GIVEN** Node X reads "file.txt" → cache miss → RPC to owner
- **WHEN** same Node X reads "file.txt" within 60s
- **THEN** metadata served from local cache (<0.1ms)
- **AND** no RPC to shard owner

#### Scenario: Read repair on stale replica
- **GIVEN** Node X reads "file.txt" from replica Node R
- **WHEN** Node R returns metadata with older VectorClock than owner
- **THEN** Node X detects staleness → propagates owner's version to R
- **THEN** R updates its BoltDB with winning version

#### Scenario: Owner down fallback
- **GIVEN** shard owner Node A is DOWN (SWIM marks SUSPECT)
- **WHEN** Node X needs metadata for shard owned by A
- **THEN** Node X tries replicas from `ring.Replicas(key, M)` in order
- **THEN** first responsive replica serves metadata

### Requirement: Shard-Aware ListObjectsV2

S3 ListObjectsV2 SHALL query only shard owners for the prefix, not all N nodes.

#### Scenario: ListObjectsV2 with prefix
- **GIVEN** client calls ListObjectsV2 with prefix "images/"
- **WHEN** Node X determines shard owners for keys matching "images/*"
- **THEN** Node X sends ListShard RPC only to those shard owners
- **THEN** RPC count = number of shard owners (not N)
- **THEN** responses merged and returned to client

### Requirement: Configuration

The following config keys SHALL be added (all with safe defaults):

```toml
[momofs]
metadata_replication = 3    # M: metadata replicas per shard (default 3)
metadata_quorum = 2         # W: write quorum (default (M/2)+1 = 2)
metadata_ttl = "60s"        # cache TTL for metadata (default 60s)
enabled = true              # false = local-only legacy mode

[global]
metadata_snapshot_interval = "0"    # 0 = disabled; e.g., "24h" for daily
metadata_backup_retention = "7d4w"  # 7 daily + 4 weekly
```

#### Scenario: Backward compatibility
- **GIVEN** config with `momofs.enabled = false`
- **WHEN** server starts
- **THEN** metadata remains local-only (no ring, no RPC, no quorum)
- **THEN** existing behavior unchanged

### Requirement: Backup/Recovery CLI

The system SHALL provide `momo backup` and `momo restore` commands with integrity verification.

#### Scenario: Automated daily snapshot
- **GIVEN** `metadata_snapshot_interval = "24h"`
- **WHEN** interval elapses
- **THEN** node streams bbolt pages to snapshot file in configured directory
- **THEN** retention policy applied (7 daily + 4 weekly)

#### Scenario: Point-in-time recovery
- **GIVEN** node DB corrupted at T+5h
- **WHEN** operator runs `momo restore --input snapshot_T --force`
- **THEN** DB restored from snapshot
- **THEN** integrity verification re-hashes all blobs vs ObjectMeta.Checksum
- **THEN** node restarts with consistent metadata

## REMOVED Requirements

None — this is an additive enhancement with full backward compatibility.

## Acceptance Criteria

1. **Write HA**: Write to any node → metadata replicated to M-1 peers via RPC → quorum ack
2. **Read HA**: Read from any node → metadata resolved via shard owner + replicas → cache hit on 2nd read
3. **ListObjects scaling**: ListObjectsV2 → only queries shard owners (O(M) not O(N))
4. **Failover**: Owner down → automatic fallback to replicas within SWIM detection window
5. **Conflict detection**: Concurrent writes → vector clock conflict logged → read repair propagates winner
6. **Config**: All new keys documented; `momofs.enabled=false` preserves legacy behavior
7. **Backup**: `momo backup` creates consistent snapshot; `momo restore` verifies integrity
8. **Tests**: Each phase has integration tests; chaos tests for failure scenarios