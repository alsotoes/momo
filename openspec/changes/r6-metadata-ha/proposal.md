# Change: R6 — Distributed Metadata Catalog with HA + Backup/Recovery

**Related Issues:**
- https://github.com/alsotoes/momo/issues/934 (R6: Metadata catalog HA + backup/recovery)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

Current MomoFS stores metadata (namespace→hash, object metadata, S3 headers) in a **per-node bbolt database** with no cross-node visibility. This creates three production-readiness gaps:

1. **No HA for metadata writes** — if a node crashes, its metadata is unavailable until recovery
2. **No backup/recovery** — no `momo backup/restore`, no automated snapshots, no documented PITR
3. **ListObjects scalability** — S3 ListObjectsV2 scatters to all N nodes (O(N) RPCs); with 100+ nodes this is prohibitive

The `distributed-metadata-v1` spec (ratified) defines the long-term architecture: **consistent hash ring + quorum replication + vector clocks** (Dynamo pattern). This change implements it incrementally in 5 phases, each shipping value.

## What Changes

### Phase 1 — Hash Ring + RPC Framework
- 256-shard consistent hash ring with 150 vnodes/node
- p2p RPC framework for metadata ops (mirrors `OPRFProvider` pattern): `PutMetadata`, `ResolveMetadata`, `ReplicateMetadata`
- gRPC-like request/response with `atomic.Uint64` request IDs, timeout handling

### Phase 2 — Quorum Writes + Vector Clocks
- `CASStore.Put` routes metadata to shard owner + M-1 replicas (configurable, default M=3)
- Quorum protocol: W=(M/2)+1 (default 2), R=1
- BoltDB schema extension: `VectorClock`, `ShardKey`, `MetadataReplicas`
- Vector clock conflict detection on concurrent writes

### Phase 3 — Read Path + Repair
- Metadata cache (TTL=60s) on all nodes — hot paths become local after 2 reads
- Read repair: if replica metadata differs, propagate winning version
- Hinted handoff for downed replicas
- Fallback to any replica if shard owner is down (SWIM SUSPECT triggers failover)

### Phase 4 — ListObjects + Config
- Shard-aware ListObjectsV2: only query shard owners for prefix, not all N nodes
- Configuration: `[momofs] metadata_replication=3 metadata_quorum=2 metadata_ttl=60s`
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

- **Affected Specs:** `specs/r6-metadata-ha/spec.md` (requirements below)
- **Performance:** 
  - ListObjectsV2: O(N) → O(shard_owners) RPCs (massive scale win)
  - Hot reads: local cache hit after 2 reads (<0.1ms)
  - Write latency: +0.5ms for quorum ack across shard replicas
- **Correctness:** Vector clocks capture true concurrency; scrub logs conflicts
- **Operations:** Full backup/restore CLI, automated snapshots, documented PITR
- **Config:** New `[momofs]` and `[global]` keys (documented in CONFIGURATION.md)

## Risk Mitigation

- **Incremental:** Each phase ships independently; `momofs.enabled=false` keeps legacy local-only mode
- **Proven pattern:** Dynamo/Cassandra/Riak quorum + vector clocks at scale
- **Reuse:** Leverages existing OPRFProvider RPC pattern, gossip/SWIM, SHA-256 hashing, BoltDB
- **Testing:** Integration tests at each phase gate; chaos tests for failure scenarios