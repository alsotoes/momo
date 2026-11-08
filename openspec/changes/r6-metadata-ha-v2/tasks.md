# Tasks — R6 v2: Distributed Metadata Catalog (Any-Node Quorum)

## Phase 1 — Core Infrastructure: Hash Ring + Any-Node RPC Framework
- [x] Implement 256-shard consistent hash ring with 150 vnodes/node in `src/p2p/metadata_ring.go`
  - `NewRing(nodes []NodeInfo) *Ring` — build ring from node list
  - `Lookup(key string) int` — return node ID owning shard (for routing hint only)
  - `Replicas(key string, M int) []int` — return M distinct replica node IDs
  - `UpdateNodes(nodes []NodeInfo)` — ring update on membership change (SWIM integration)
- [x] Implement metadata RPC framework in `src/p2p/metadata_rpc.go` (mirrors `OPRFProvider` pattern)
  - Msg types: `MsgPutMetadata`, `MsgResolveMetadata`, `MsgReplicateMetadata`
  - `MetadataRPCProvider` with `HandleRPC`, request ID (`atomic.Uint64`), timeout
  - Request/Response payload types with `VectorClock`, `ShardKey`, `MetadataReplicas`
  - Panic recovery with `syscall.EIO` (Rule 24)
- [x] Wire RPC transport: register `MetadataRPCProvider` in `Gossiper` consumer loop
- [x] Unit tests: `metadata_ring_test.go` (ownership stability, replica distribution)

## Phase 2 — Any-Replica Quorum Writes + Vector Clocks
- [x] Extend `ObjectMeta` in `src/storage/storage.go` with `VectorClock`, `ShardKey`, `MetadataReplicas`
- [x] Implement `PutMetadata` RPC handler (accepts write on ANY replica, not just owner):
  - Write to local BoltDB (with extended ObjectMeta)
  - Increment local vector clock entry on each write
  - Return ack with updated VClock
- [x] Implement `ReplicateMetadata` handler on replicas:
  - Write received ObjectMeta to local BoltDB
  - Return ack
- [x] Add `PutWithMetadata` to `CASStore` for distributed metadata writes
  - Accepts VectorClock, ShardKey, MetadataReplicas
  - Preserves backward compatibility with existing `Put` calls
- [x] Update `metadata_rpc.go` to call `store.PutWithMetadata`
- [x] Vector clock handling in ObjectMeta encode/decode with backward compatibility
- [x] Config: `[momofs] metadata_replication`, `metadata_quorum`, `enabled` (parsing in config.go)
- [x] **Quorum write protocol**: concurrent RPC to all M replicas, wait for
  W=(M/2)+1 acks (`PutMetadata`); the local replica applies in-process, remotes
  are awaited with per-request correlation (`replicaPending`) and a bounded
  timeout. Partial success is an error, never a false ack.
- [x] **Vector clock advance on write**: the writer bumps its own clock entry
  before fan-out, and the advanced clock is returned to the caller
- [x] **Shard key and replica set resolution**: derived from the ring when the
  caller omits them
- [x] **Fail-closed replica validation**: a replica rejects nameless/hashless
  records with `EBADMSG` instead of storing an unreachable record
- [x] **Encoding fix found by this work**: the v2 metadata shard-key header was
  written with a payload-sized buffer, appending `len(shardKey)` zero bytes and
  corrupting every following field — every non-empty shard key failed to decode
  (`truncated replica data`). Fixed and covered by a round-trip table
  (ASCII, multibyte, combining marks) plus a store-level test.
- [x] **Tests**: quorum reached with local + one ack, quorum failure below W,
  single-replica local-only write, derived shard/replica set, nameless record
  rejected, unknown-request reply ignored, race-free concurrent fan-out
- [x] **Daemon write path**: `getFile` publishes every successful PUT through the
  quorum writer (`publishMetadataToQuorum`); a quorum failure fails the request
  with `EIO` so the client retries, while the local copy stays and the retry is
  idempotent. Enabled only when `[momofs] enabled=true` and the metadata
  replication factor is > 1; otherwise the publisher is nil and writes are
  unchanged (single-node deployments unaffected).
- [ ] **Vector clock conflict detection** on read (compare VClocks from multiple replicas) — Phase 3 read path
- [ ] **Integration tests**: 3-node cluster, concurrent writes → conflict detection, quorum with 1 replica down — needs the read path to observe conflicts

## Phase 3 — Any-Node Read Path + Repair
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
- [ ] **Any-node read**: on cache miss, parallel ResolveMetadata RPC to ALL M replicas
- [ ] **Read repair**: on version mismatch, compare VectorClocks → propagate winner to stale replicas
- [ ] **Hinted handoff**: owner stores hints for downed replicas; on recovery, replay hints
- [ ] **SWIM integration**: on SUSPECT/OFFLINE for replica, invalidate cache entries, failover to other replicas
- [ ] Integration tests: owner down → fallback to replica; cache hit after 2 reads; read repair on stale replica

## Phase 4 — ListObjects + Config + Backward Compat
- [x] Implement shard-aware `ListShard` RPC:
  - Args: `ShardKey`, `Prefix`, `Delimiter`, `MaxKeys`, `ContinuationToken`
  - Reply: `FileMetadata[]`, `CommonPrefixes[]`, `NextContinuationToken`
- [x] Modify S3 ListObjectsV2 handler (`s3_communicator.go`):
  - When `momofs.enabled=true`: determine shard owners for prefix
  - Fan-out `ListShard` RPC to shard owners only
  - Merge responses → return to client
- [x] Add config keys: `metadata_ttl`, `[global] metadata_snapshot_interval`, `metadata_backup_retention`
- [x] Backward compat: `momofs.enabled=false` → skip all distributed logic, use local `CASStore.List()`
- [x] Update `docs/GUIDES/CONFIGURATION.md`, `conf/momo.conf` with new keys
- [x] Integration tests: ListObjectsV2 with prefix on 10-node cluster → O(M) RPCs verified

## Phase 5 — Backup/Recovery (R6a)
- [x] Implement `momo backup` CLI in `src/momo.go`:
  - `backupCmd` with flags `--output`, `--compress`
  - Stream bbolt pages using `bbolt.Tx.WriteTo()` (online, non-blocking)
  - Write to file(s) with optional gzip compression
  - Include metadata: timestamp, node ID, DB version
- [x] Implement `momo restore` CLI:
  - `restoreCmd` with flags `--input`, `--force`
  - Validate backup header + checksum
  - `--force` required to overwrite existing DB
  - Restore pages to new DB file
- [x] Automated periodic snapshots:
  - Background goroutine in `CASStore` (or server) triggered by `metadata_snapshot_interval`
  - Write to configured directory with rotation (daily/weekly)
  - Retention policy: 7 daily + 4 weekly (`metadata_backup_retention`)
- [x] Point-in-time recovery documentation:
  - `docs/BACKUP_RECOVERY.md`: stop node → restore → verify → restart
  - Integrity verification: re-hash all blobs vs `ObjectMeta.Checksum`
- [x] Update `docs/GUIDES/CONFIGURATION.md`, `conf/momo.conf` with backup keys
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
- Chaos test: kill replica during write/read → verify correctness
- Benchstat: no regression on metadata ops (target <1ms p99 for cache hit)
- Reviewer ✅ gate (Rule 55)