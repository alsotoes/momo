# Tasks — R6: Distributed Metadata Catalog with HA + Backup/Recovery

## Phase 1 — Core Infrastructure: Hash Ring + RPC Framework
- [x] Implement 256-shard consistent hash ring with 150 vnodes/node in `src/p2p/metadata_ring.go`
  - `NewRing(nodes []NodeInfo) *Ring` — build ring from node list
  - `Lookup(key string) int` — return node ID owning shard
  - `Replicas(key string, M int) []int` — return M distinct replica node IDs
  - `UpdateNodes(nodes []NodeInfo)` — ring update on membership change (SWIM integration)
- [x] Implement metadata RPC framework in `src/p2p/metadata_rpc.go` (mirrors `OPRFProvider` pattern)
  - Msg types: `MsgPutMetadata`, `MsgResolveMetadata`, `MsgReplicateMetadata`
  - `MetadataRPCProvider` with `HandleRPC`, request ID (`atomic.Uint64`), timeout
  - Request/Response payload types with `VectorClock`, `ShardKey`, `MetadataReplicas`
  - Panic recovery with `syscall.EIO` (Rule 24)
- [x] Wire RPC transport: register `MetadataRPCProvider` in `Gossiper` consumer loop
- [x] Unit tests: `metadata_ring_test.go` (ownership stability, replica distribution)

## Phase 2 — Quorum Writes + Vector Clocks
- [x] Extend `ObjectMeta` in `src/storage/storage.go` with `VectorClock`, `ShardKey`, `MetadataReplicas`
- [x] Implement `PutMetadata` RPC handler on shard owner:
  - Write to local BoltDB (with extended ObjectMeta)
  - Increment local vector clock entry on each write
- [x] Implement `ReplicateMetadata` handler on replicas:
  - Write received ObjectMeta to local BoltDB
  - Return ack
- [x] Add `PutWithMetadata` to `CASStore` for distributed metadata writes
  - Accepts VectorClock, ShardKey, MetadataReplicas
  - Preserves backward compatibility with existing `Put` calls
- [x] Update `metadata_rpc.go` to call `store.PutWithMetadata`
- [x] Vector clock handling in ObjectMeta encode/decode with backward compatibility
- [x] Config: `[momofs] metadata_replication`, `metadata_quorum`, `enabled` (parsing in config.go not yet done)
- [ ] Quorum write protocol: async replicate to M-1, wait for W=(M/2)+1 acks
- [ ] Vector clock conflict detection on read
- [ ] Integration tests: 3-node cluster, concurrent writes → conflict detection, quorum with 1 replica down

## Phase 3 — Read Path + Repair
- [x] Implement `ResolveMetadata` RPC handler in `metadata_rpc.go`:
  - Look up local BoltDB by name → return ObjectMeta
  - Added ModTime field to ResolveMetadataReply
- [x] Implement metadata cache in `CASStore`:
  - TTL=60s (configurable `metadata_ttl`)
  - LRU eviction with max entries (10k)
  - Methods: `getCachedMeta`, `setCachedMeta`, `invalidateCache`
- [x] Modify `CASStore.GetMeta` / server read path:
  - Added `getCachedMeta`, `setCachedMeta`, `invalidateCache` methods
  - Added `GetMetaWithCache` for distributed reads with cache
  - Added `resolveMetadataViaRPC` stub for future RPC integration
- [x] Cache invalidation on writes:
  - `PutWithMetadata` calls `invalidateCache(name)`
  - `Delete` calls `invalidateCache(name)`
- [ ] Read repair:
  - On cache miss, if multiple replicas queried and versions differ
  - Compare VectorClocks → propagate winning version to stale replicas
- [ ] Hinted handoff:
  - Owner stores hints for downed replicas (in-memory + persisted)
  - On replica recovery, owner replays hints → hint deleted
- [ ] SWIM integration: on SUSPECT/OFFLINE for shard owner, invalidate cache entries, failover to replicas
- [ ] Integration tests: owner down → fallback to replica; cache hit after 2 reads; read repair on stale replica

## Phase 4 — ListObjects + Config + Backward Compat
- [ ] Implement shard-aware `ListShard` RPC:
  - Args: `ShardKey`, `Prefix`, `Delimiter`, `MaxKeys`, `ContinuationToken`
  - Reply: `FileMetadata[]`, `CommonPrefixes[]`, `NextContinuationToken`
- [ ] Modify S3 ListObjectsV2 handler (`s3_communicator.go`):
  - When `momofs.enabled=true`: determine shard owners for prefix
  - Fan-out `ListShard` RPC to shard owners only
  - Merge responses → return to client
- [ ] Add config keys: `metadata_ttl`, `[global] metadata_snapshot_interval`, `metadata_backup_retention`
- [ ] Backward compat: `momofs.enabled=false` → skip all distributed logic, use local `CASStore.List()`
- [ ] Update `docs/CONFIGURATION.md`, `conf/momo.conf` with new keys
- [ ] Integration tests: ListObjectsV2 with prefix on 10-node cluster → O(M) RPCs verified

## Phase 5 — Backup/Recovery (R6a)
- [ ] Implement `momo backup` CLI in `src/momo.go`:
  - `backupCmd` with flags `--output`, `--compress`
  - Stream bbolt pages using `bbolt.Tx.Page()` (online, non-blocking)
  - Write to file(s) with optional gzip compression
  - Include metadata: timestamp, node ID, DB version
- [ ] Implement `momo restore` CLI:
  - `restoreCmd` with flags `--input`, `--force`
  - Validate backup header + checksum
  - `--force` required to overwrite existing DB
  - Restore pages to new DB file
- [ ] Automated periodic snapshots:
  - Background goroutine in `CASStore` (or server) triggered by `metadata_snapshot_interval`
  - Write to configured directory with rotation (daily/weekly)
  - Retention policy: 7 daily + 4 weekly (`metadata_backup_retention`)
- [ ] Point-in-time recovery documentation:
  - `docs/BACKUP_RECOVERY.md`: stop node → restore → verify → restart
  - Integrity verification: re-hash all blobs vs `ObjectMeta.Checksum`
- [ ] Integration test: write data → backup → corrupt DB → restore → verify all data intact + checksums match
- [ ] Update `docs/ARCHITECTURE.md` § with backup/recovery section

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76 (one per phase or combined)
- [ ] ADR sync (Rule 78) — new ADR for each phase or combined
- [ ] PR body with `Resolves #934` (Phase 5) + phase-specific tracking

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos test: kill owner during write/read → verify correctness
- Benchstat: no regression on metadata ops (target <1ms p99 for cache hit)
- Reviewer ✅ gate (Rule 55)