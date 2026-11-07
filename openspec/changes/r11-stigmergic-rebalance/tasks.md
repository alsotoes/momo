# Tasks — R11: Auto-Rebalance via Stigmergic Local Rules

## Phase 1 — Core Infrastructure: Local Rule Engine
- [x] Local rule engine — `src/server/rebalance.go` (`startRebalanceLoop` + `rebalanceCycle`), wired in `server.go`; no coordinator, no separate package (Rule 74 seam is the `rebalancePusher` interface)
  - `RebalanceController` with nodeID, store, peers, cmap, config
  - `Run(ctx)` — background goroutine with ticker
  - `runHomeostaticChecks()` — disk pressure, replication factor, domain spread
- [x] Blob shedding — `rebalanceCycle` picks coldest local blobs (modtime proxy), pushes to a placement peer, and drops local bytes only after the peer ack (`DaemonRebuildSource.Push`); `CASStore.ListBlobs`/`DropBlob`/`GetBlobByHash` + `LocalBlobStore.WalkBlobs`
  - `shedToPeer(peer)` — select cold blobs, stream with semaphore + bandwidth limiter
  - `selectColdBlobs(n)` — least accessed via access stats (or LRU)
  - `streamBlobToPeer(hash, peer)` — P2P transport with rate limiting
- [x] Re-replication on leave — covered by the R2 `rebuildLoop` (repairs any blob below target from a verified survivor); R11 adds no duplicate path
  - `OnPeerLeave(departedID)` — scan metadata, trigger re-replication
  - `reReplicateObject(obj)` — find survivor, select new target, stream
  - `selectSurvivor/replica` — from MetadataReplicas excluding departed
- [ ] Failure-domain migration — blocked: needs per-blob replica location records. Tracked in #1260. R1 spreads new writes and R2 `Restore` prefers another domain on repair meanwhile.
  - `checkFailureDomainSpread()` — detect >1 replica in same domain
  - `migrateToDifferentDomain(obj)` — CRUSH target selection
- [x] Config parsing: `[rebalance]` section (`enabled`, `interval`, `disk_high_water`, `disk_low_water`, `max_shed_per_cycle`) with validation and defaults
  - `enabled`, `interval`, `max_concurrent`, `max_bandwidth_mbps`
  - `disk_high_water`, `disk_low_water`, `request_rate_ratio`
- [x] Unit tests: storage blob listing/drop, config parsing/validation, cycle behavior (shed cap, below high water, push failure, no peer)

## Phase 2 — SWIM Integration + Membership Events
- [x] Wire `OnPeerJoin` in `bootstrapP2P` (`joinHandler` + `newJoinMigrator`); `OnPeerLeave` deliberately left to the R2 rebuild loop
  - `gossip.OnJoin` → `rc.OnPeerJoin(peer.ID)`
  - `gossip.OnLeave` → `rc.OnPeerLeave(peer.ID)`
- [x] Implement `OnPeerJoin` — `migrateToPeer` moves blobs the joining peer now owns and this node no longer owns
  - Recompute CRUSH for local objects
  - Identify objects where new peer is target
  - Trigger shed for those objects
- [x] Implement `OnPeerLeave` — intentionally omitted: the R2 rebuild loop re-replicates missing replicas
  - Find objects where departed was replica
  - Trigger re-replication for each
- [x] Integration test: joining peer receives the migrated blob over a real TCP push and the sender drops only after the ack (`TestRebalanceJoinMigrationEndToEnd`, 3-node placement)

## Phase 3 — Homeostatic Checks (Continuous)
- [x] Disk pressure shedding — `rebalanceCycle` (coldest-first, high/low water, per-cycle cap)
  - `checkDiskPressure()` — local usage vs peer usage (via gossip)
  - `selectColdBlobs()` — use access stats or LRU approximation
  - Rate limiting: semaphore + token bucket bandwidth limiter
- [x] Replication factor maintenance — covered by the R2 rebuild loop
  - `checkReplicationFactor()` — scan metadata for under-replicated
  - Trigger re-replication with new CRUSH target
- [ ] Failure domain spread — see the note above (needs replica tracking; new writes already spread by R1). Tracked in #1260.
  - `checkFailureDomainSpread()` — analyze MetadataReplicas domains
  - Migrate one replica if concentrated
- [ ] Request rate redirection — blocked: needs a gossip load signal and a redirect contract. Tracked in #1261.
- [x] Rebalance observability — `momo_rebalance_{blobs_moved,bytes_moved,cycles,failures}_total` counters recorded on the push choke point
  - Extend existing proxy path: if peer has blob + lower load, redirect
- [x] Integration test: real TCP push through dial, handshake, metadata, streamed payload, ACK (`TestRebalancePushEndToEnd`); caught a stale handshake timestamp on the peer dial path

## Phase 4 — Degraded Read + Throttling + Polish
- [x] Degraded read during rebalance — R2 survivor-set read fallback, required before the loop will start
  - If blob not found on current replicas, try pre-rebalance CRUSH targets
  - Update `getFile` / `DownloadWithFallback` to try old placement
- [x] Streaming push — `BlobSize` + `pushReaderToPeer`/`writeThrottledReader`; no `io.ReadAll` on the rebalance path
- [x] Bandwidth throttling — `max_bandwidth_mbps` token bucket (`writeThrottled` + `bandwidthLimiter`) on every rebalance push; 0 = unlimited
  - Token bucket limiter in `shed.go` / `replicate.go`
  - Foreground traffic priority (separate semaphore or priority queue)
- [x] Config validation + defaults in `config.go` (`[rebalance]`)
- [x] Update `docs/GUIDES/CONFIGURATION.md`, `conf/momo.conf`
- [x] Update `docs/ARCHITECTURE.md` with the stigmergic rebalance section
- [x] Blog post 062 (Rule 76)

## Phase 5 — Chaos Testing + Validation
- [x] Chaos test: 8 seeds × 60 randomized shed/join rounds over 5 nodes with 30% push failures (`TestRebalanceChaosNeverLosesTheLastCopy`). It found a real data-loss bug: a node that had shed a blob kept its metadata, so a later push acknowledged without storing bytes and the sender dropped the last copy. Fixed by the `blobPresence` check in `Put`.
  - Verify convergence to balanced state (disk usage, replica count)
  - Verify no data loss (all objects have RF replicas)
  - Verify read availability throughout (degraded read works)
- [x] Benchmarks: `BenchmarkRebalanceCycle` (evaluation cost, 1000 blobs ≈ 25 ms/pass), `BenchmarkPickShedTarget` (~1.4 µs), `BenchmarkWriteThrottled` (limiter adds ≈1.2 µs per MiB at 10 Gbps). End-to-end foreground throughput still needs a multi-node load harness; the benchmarks bound the per-node overhead instead.
  - Target: <5% regression on client read/write
- [x] Reviewer ✅ gate (Rule 55) — per PR

## Cross-Phase Requirements
- [x] `go build ./...` clean at each phase
- [x] `go test ./...` passes at each phase
- [x] `go vet`, `gofmt -l` clean
- [x] `go work vendor` parity (Rule 25)

- [x] ADR sync (Rule 78) — ADR 0072
- [x] PR body references the tracking issues without closing the umbrella (R11 is phased)

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos test: random membership changes → cluster converges
- Benchstat: no regression on client ops during rebalance
- Reviewer ✅ gate (Rule 55)