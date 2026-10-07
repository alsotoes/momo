# Change: R6 — Distributed Metadata Catalog with HA + Backup/Recovery (v2: Any-Node Quorum)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/934 (R6: Metadata catalog HA + backup/recovery)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current MomoFS stores metadata (namespace→hash, object metadata, S3 headers) in a **per-node bbolt database** with no cross-node visibility. This creates three production-readiness gaps:

1. **No HA for metadata writes** — if a node crashes, its metadata is unavailable until recovery
2. **No backup/recovery** — no `momo backup/restore`, no automated snapshots, no documented PITR
3. **ListObjects scalability** — S3 ListObjectsV2 scatters to all N nodes (O(N) RPCs); with 100+ nodes this is prohibitive

The `distributed-metadata-v1` spec (ratified) defines a shard-owner + replica model. **This v2 revision aligns with core principles**: "Read From Any Node" and "Zero SPOF" require **any-replica quorum writes** and **any-node reads**, not a designated shard owner.

## What Changes

### Core Principle Alignment

| Principle | v1 Design | v2 Design (This Spec) |
|-----------|-----------|----------------------|
| **Read From Any Node** | Client → shard owner → replicas | Any node serves read; local cache hit or transparent proxy |
| **Zero SPOF** | Shard owner = soft coordinator | No coordinator; quorum to any M replicas |
| **Stigmergy** | Central ring lookup | Local rules: cache, replicate, repair |

### Phase 1 — Hash Ring + Any-Node RPC Framework
- 256-shard consistent hash ring with 150 vnodes/node (for *routing hints only*, not ownership)
- p2p RPC framework for metadata ops: `PutMetadata`, `ResolveMetadata`, `ReplicateMetadata`
- **Any-node semantics**: RPC can be sent to ANY node in replica set, not just "owner"

### Phase 2 — Quorum Writes + Vector Clocks (Any-Replica)
- `CASStore.PutMetadata` routes to **all M replicas** concurrently
- Quorum protocol: W=(M/2)+1 acks from **any** replicas (not owner-coordinated)
- BoltDB schema: `VectorClock`, `ShardKey`, `MetadataReplicas` (for hinted handoff)
- Vector clock conflict detection on concurrent writes to any replica

### Phase 3 — Read Path + Repair (Any-Node)
- Metadata cache (TTL=60s) on all nodes — hot paths become local after 2 reads
- Read repair: if replica metadata differs, propagate winning version
- Hinted handoff for downed replicas (stored on any peer that got the write)
- Fallback to any replica if first choice is down (SWIM SUSPECT triggers failover)

### Phase 4 — ListObjects + Config
- Shard-aware ListObjectsV2: query **all shard owners for prefix** (not all N nodes)
- Config: `[momofs] metadata_replication=3 metadata_quorum=2 metadata_ttl=60s`
- Backward compatibility: `momofs.enabled=false` → local-only mode unchanged

### Phase 5 — Backup/Recovery (R6a)
- `momo backup [--output dir] [--compress]` — streaming bbolt page backup
- `momo restore [--input file] [--force]` — safe restore with integrity verification
- Automated periodic snapshots via `[global] metadata_snapshot_interval`
- Point-in-time recovery documentation and integration test

## Non-Goals

- WAL integration for crash recovery (Phase 2 roadmap item)
- Per-tenant metadata replication (Phase 4 GDPR feature)
- Erasure coding compatibility (Phase 7)
- Full FUSE mount integration with distributed metadata (already local)

## Impact

- **Performance:** 
  - ListObjectsV2: O(N) → O(shard_owners) RPCs (massive scale win)
  - Hot reads: local cache hit after 2 reads (<0.1ms)
  - Write latency: +0.5ms for quorum ack across M replicas (any replicas)
- **Correctness:** Vector clocks capture true concurrency; scrub logs conflicts
- **Operations:** Full backup/restore CLI, automated snapshots, documented PITR
- **Config:** New `[momofs]` and `[global]` keys (documented in CONFIGURATION.md)

## Risk Mitigation

- **Incremental:** Each phase ships independently; `momofs.enabled=false` keeps legacy local-only mode
- **Proven pattern:** Dynamo/Cassandra/Riak quorum + vector clocks at scale
- **Reuse:** Leverages existing OPRFProvider RPC pattern, gossip/SWIM, SHA-256 hashing, BoltDB
- **Testing:** Integration tests at each phase gate; chaos tests for failure scenarios