# 0038-r6-metadata-ha

## Status
Proposed

## Confidence
Medium

## Context
Current MomoFS stores metadata (namespace→hash, object metadata, S3 headers) in a **per-node bbolt database** with no cross-node visibility. This creates three production-readiness gaps:

1. **No HA for metadata writes** — if a node crashes, its metadata is unavailable until recovery
2. **No backup/recovery** — no `momo backup/restore`, no automated snapshots, no documented PITR
3. **ListObjects scalability** — S3 ListObjectsV2 scatters to all N nodes (O(N) RPCs); with 100+ nodes this is prohibitive

The `distributed-metadata-v1` spec (ratified) defines the long-term architecture: **consistent hash ring + quorum replication + vector clocks** (Dynamo pattern). This change implements it incrementally in 5 phases, each shipping value.

## Decision
- Consistent Hash Ring for Metadata Sharding: The system SHALL provide a 256-shard consistent hash ring with 150 virtual nodes per physical node to deterministically assign metadata shard ownership.
- Metadata RPC Framework: The system SHALL provide a p2p RPC framework for metadata operations with request/response semantics, timeout handling, and panic recovery.
- Quorum Protocol with Vector Clocks: The system SHALL implement a quorum write protocol with W=(M/2)+1 and vector clock conflict detection.
- BoltDB Schema Extension: The `ObjectMeta` struct SHALL be extended with distributed metadata fields.
- Read Path with Cache + Repair: The system SHALL provide a read path with local metadata cache (TTL=60s) and automatic read repair.
- Cluster-Wide ListObjectsV2: S3 ListObjectsV2 SHALL return objects held by the whole cluster, not only those whose metadata happens to live on the node that served the request.
- Configuration: The following config keys SHALL be added (all with safe defaults): ```toml [momofs] metadata_replication = 3    # M: metadata replicas per shard (default 3) metadata_quorum = 2         # W: write quorum (default (M/2)+1 = 2) metadata_ttl = "60s"        # cache TTL for metadata (default 60s) enabled = true              # false = local-only legacy mode [global] metadata_snapshot_interval = "0"    # 0 = disabled; e.g., "24h" for daily metadata_backup_retention = "7d4w"  # 7 daily + 4 weekly ```
- Backup/Recovery CLI: The system SHALL provide `momo backup` and `momo restore` commands with integrity verification.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Partial
- **Blog post**: 

## References
- Issue: #934
- PR: 
- Spec: `openspec/changes/r6-metadata-ha/`
- Blog: 

