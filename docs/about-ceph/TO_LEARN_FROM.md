# TO_LEARN_FROM.md — What to Learn from Ceph for Your Own Filesystem

## 1. Bypass the Filesystem Layer Entirely (CRITICAL)

### Why it matters
- **FileStore**: POSIX filesystem on raw devices → "journaling of journal" → 3x write amplification
- **BlueStore**: Bypasses filesystem → direct raw block access → embedded RocksDB for metadata → 2x faster writes, much better tail latency
- **Lesson**: Don't add a filesystem layer between your engine and raw devices. Direct block device access + embedded KV store is vastly more efficient.

### How to implement
- Use `io_uring` or direct `read`/`write` syscalls to access block devices
- Embed RocksDB (or alternative KV store) for metadata management
- Avoid FUSE, ext4, xfs, or any filesystem intermediary
- If you need partitioning, manage it yourself rather than relying on filesystem features

### Ceph reference
- BlueStore stores object data directly on raw blocks; RocksDB manages onodes/extents
- No double-write penalty that FileStore suffers
- Small sequential reads using raw librados are slower, but higher-level interfaces (RBD, CephFS) are great

---

## 2. Design a Three-Device Hierarchy

### Why it matters
- **Device types**: `block` (data), `block.db` (metadata/DB), `block.wal` (write-ahead log)
- **Colocation**: Default — all on one device. Fastest device gets DB+WAL
- **Separation**: SSD for DB+WAL, HDD for data → optimal performance/device-count tradeoff
- **Lesson**: Support multiple device tiers with automatic colocation logic from day one.

### Configuration options (from Ceph config ref)
```
# Single device (colocated — default)
block = /dev/sda

# Two devices: data + fast metadata
block = /dev/hdd1
block.db = /dev/ssd1

# Three devices: data + DB + WAL (optimal)
block = /dev/hdd1
block.db = /dev/ssd1
block.wal = /dev/nvme1n1
```

### Auto-colocation logic
- If only small fast space available (< 1 GB): use as WAL device
- If more fast space available: provision DB device (WAL implicitly colocated with DB)
- If mix of fast/slow devices: `block.db` on faster, `block` on slower (rotational drives)

### How to implement
- Detect device rotational status (HDD vs SSD/NVMe)
- Prioritize fastest device for metadata (DB + WAL)
- Allow flexible configuration: single, dual, or triple device setups
- Default to colocation when device types are mixed or unknown

---

## 3. Implement Selective Write-Ahead Journaling

### Why it matters
- **BlueStore WAL**: Only journals writes < `min_alloc_size` (64 KiB default). Larger writes go directly to main device.
- **Crimson journal**: Fixed-size circular buffer, record-based with deltas+extents
- **Crash recovery**: WAL replay on restart ensures no acknowledged write is lost
- **Lesson**: Don't journal everything. Only journal small/metadata writes; large data writes bypass for performance.

### BlueStore WAL lifecycle
1. Client write received by OSD
2. Data written to WAL (journal device or main device)
3. Acknowledge to client
4. Data committed to final location on main block device
5. WAL entry removed after successful commit
6. On crash between steps 2-3: WAL replays on restart to complete committed writes

### Configuration knobs
```
# WAL size (typically 512 MB to 4 GB per OSD)
bluestore_block_wal_size = 2147483648  # 2GB

# Only journal writes smaller than this (default 64 KiB)
bluestore_min_alloc_size = 65536

# Disable deferred writes for maximum safety
bluestore_prefer_deferred_size = 0

# Sync metadata on every commit (slower, safer)
bluestore_sync_submit_transaction = true
```

### How to implement
- Track write size: ≤ threshold → write through journal; > threshold → direct to main device
- Implement WAL replay on startup for crash recovery
- Configure threshold based on device type (smaller for SSDs, larger for HDDs)
- Track `committed_to` state for each transaction (like Crimson's `record_submitter.update_committed_to()`)

---

## 4. Manage Write Amplification (WAF)

### Why it matters
- **Key metric**: WAF (Write Amplification Factor). Ceph papers show 13x+ amplification regardless of backend
- **BlueStore**: Partial overwrites < chunk size → log to RocksDB WAL → dominant WAF source
- **Crimson**: Log-structured design → minimizes amplification for NVMe/SSD
- **Lesson**: Track and optimize write amplification. Small writes = biggest amplification risk.

### Write categories from MSST 2017 paper
- **FileStore**: External Ceph journal triples write traffic
- **BlueStore**: RocksDB WAL dominant source (partial overwrites < chunk size)
- **KStore**: Compaction overhead from underlying KV stores (worst WAF)

### Configuration for WAF reduction
```
# BlueStore: reduce chunk size to reduce partial overwrite amplification
bluestore_chunk_size = 4096  # smaller = less amplification but more metadata

# Crimson: configure batch flushing to reduce small writes
seastore_journal_batch_capacity = 4096
seastore_journal_batch_flush_size = 256*1024*1024  # 256MB

# Cache configuration to reduce reads that trigger rewrites
bluestore_cache_meta_ratio = 0.5  # 50% of cache for metadata
bluestore_cache_size_ssd = 8589934592  # 8GB for SSD OSDs
```

### How to implement
- Classify write traffic: small random, large sequential, metadata, data
- Track WAF per category (don't just report overall)
- Optimize chunk/allocation unit size for target workload
- Implement batch flushing for small writes (accumulate then flush)
- Cache hot metadata to reduce reads that trigger rewrites

---

## 5. Embed a KV Store for Metadata (Don't Build From Scratch)

### Why it matters
- **RocksDB embedded KV store**: Onodes, extent trees, allocation maps, compaction
- **Dynamic allocation**: `bluestore_min_alloc_size` differs for HDD vs SSD (4KiB vs 512B default)
- **Cache hierarchy**: `bluestore_cache_meta`, `cache_kv`, data cache with configurable ratios
- **Lesson**: Embed a proven KV store for metadata (don't build from scratch). Manage allocation units and caching explicitly.

### RocksDB configuration (from Ceph tuned performance)
```
bluestore_rocksdb_options = compression=kNoCompression,
  max_write_buffer_number=32,
  min_write_buffer_number_to_merge=2,
  recycle_log_file_num=32,
  compaction_style=kCompactionStyleLevel,
  write_buffer_size=67108864,
  target_file_size_base=67108864,
  max_background_compactions=31,
  level0_file_num_compaction_trigger=8,
  level0_slowdown_writes_trigger=32,
  level0_stop_writes_trigger=64,
  max_bytes_for_level_base=536870912,
  compaction_threads=32,
  max_bytes_for_level_multiplier=8,
  flusher_threads=8,
  compaction_readahead_size=2MB
```

### Cache ratios for best performance
```
# Best results from tuned configuration:
bluestore_cache_size_ssd = 8589934592  # 8GB out of 12GB available
bluestore_cache_meta_ratio = 0.5     # 50% for Onode/cache_meta

# Cache subdivisions (when autotune disabled):
cache_meta   : BlueStore Onode and associated data
cache_kv     : RocksDB block cache (indexes/bloom-filters)
data cache   : BlueStore cache for data buffers
```

### How to implement
- Don't build your own B-tree, extent tree, or allocation map
- Embed RocksDB (or Badger, Bolt, or embedded MMKV for Go projects)
- Configure write buffers, compaction style, and background threads
- Subdivide cache into: metadata, KV/index, data buffers
- Tune ratios based on workload (more metadata cache for random workloads)

---

## 6. Design Explicit Crash Recovery Protocol

### Why it matters
- **BlueStore**: WAL → acknowledge → commit → clear sequence. On crash: WAL replays to complete committed writes
- **Crimson**: Journal replay from `written_to` to `alloc_tail`; `record_submitter.update_committed_to()` tracks state
- **Lesson**: Design explicit crash-recovery protocol. Write-ahead → acknowledge → finalize → cleanup pattern is proven.

### BlueStore crash recovery sequence
1. Write data to WAL (journal)
2. Acknowledge to client (write considered "acked")
3. Commit data to final location on main device
4. Remove WAL entry
5. On OSD crash/restart: WAL replay completes any acked-but-not-committed writes

### Crimson crash recovery
1. Replay journal deltas from `written_to` to `alloc_tail`
2. Reconstruct state from journal records
3. `record_submitter.update_committed_to()` updates committed epoch/sequence

### Implementation requirements
- **WAL file format**: sequence number, operation type, data, checksum
- **Recovery on startup**: scan WAL, determine which writes were committed vs acked
- **Idempotent operations**: replaying same WAL entry multiple times is safe
- **Truncation**: After successful commit, truncate/remove WAL entry
- **Configuration**: `wal_replay_on_boot` flag; `wal_max_size` limit

---

## 7. Enable Multi-Core Scalability

### Why it matters
- **Classic Ceph**: Thread-per-op with lock contention → scales poorly past ~16 cores
- **Crimson/SeaStore**: Seastar framework → one thread/core, sharded PGs, zero cross-core locking
- **Lesson**: For multi-core scalability, partition state by shard and minimize inter-core communication.

### Classic OSD threading model
```
Messenger thread → reads from wire → OP queue
osd-op thread-pool → picks up message → creates transaction → queues to BlueStore
kv_queue → picks up transaction → waits for RocksDB → places completion callback
finisher thread → picks up callback → queues to messenger → sends reply
```

### Crimson Seastar model
```
One thread per core (Seastar reactor)
All operations for a specific PG pinned to single shard
Zero cross-core locking (state partitioned)
Operations complete on single core without context switches (if device allows)
```

### Implementation strategies
- **Shard/PG partitioning**: Map each Placement Group to a specific core/shard
- **Per-core data structures**: Avoid global locks; use per-shard allocators
- **Asynchronous I/O**: Use `io_uring`, `aio`, or Seastar's async I/O (Go: channels + select, io_uring)
- **Minimize cross-shard communication**: Co-locate related data on same shard
- **Lock-free data structures**: Where possible, use atomic operations instead of mutexes

### Go-specific approach (no Seastar equivalent)
- **Goroutine pools** with selective blocking (vs. Seastar's zero-blocking)
- **Custom event loops** (epoll/kqueue/io_uring) instead of C++ templates
- **Per-core dispatch**: `runtime.NumProcs()` workers; each handles subset of PGs
- **Channel-based sharding**: `map[shardID]chan Message`; each shard processes its own channel
- **Avoid global locks**: Use `sync.Map` or per-shard maps with careful design

---

## 8. Build Configuration Flexibility

### Why it matters
- **Device mixing**: `block.db` on SSD, `block` on HDD; `block.wal` separate or auto-colocated
- **Tunable thresholds**: `min_alloc_size`, WAL size, batch capacities, cache ratios
- **Lesson**: Make your filesystem configurable for different device types and workloads without code changes.

### Ceph configuration examples

**Single device (all colocation):**
```
# /dev/sda used for data, DB, and WAL automatically
block = /dev/sda
```

**Two devices: HDD data + SSD metadata:**
```
block = /dev/hdd1
block.db = /dev/ssd1
# WAL auto-colocated with DB on ssd1
```

**Three devices: optimal performance:**
```
block = /dev/hdd1          # HDD for user data
block.db = /dev/ssd1       # SSD for RocksDB metadata
block.wal = /dev/nvme1n1   # NVMe for write-ahead log
```

**Configurable thresholds:**
```
# Allocation unit size (varies by device type)
bluestore_min_alloc_size_hdd = 4096
bluestore_min_alloc_size_ssd = 512

# WAL size (adjust based on workload)
bluestore_block_wal_size = 1073741824  # 1GB

# Batch controls (Crimson-style)
seastore_journal_batch_capacity = 4096
seastore_journal_batch_flush_size = 256*1024*1024
```

### How to implement
- Detect device type at startup (rotational vs non-rotational)
- Provide config file/schema for: device paths, sizes, thresholds, ratios
- Default to sensible colocation when devices are mixed/unknown
- Allow runtime reconfiguration or restart with new config
- Document all configuration options with rationale (like Ceph does)

---

## 9. Learn From Ceph's Evolution Mistakes

### What NOT to do (from 10+ years of Ceph development)

1. **Don't layer journal on journal** (FileStore's fatal flaw)
   - External Ceph journal + internal filesystem journal = 3x write amplification
   - BlueStore fixed this by bypassing filesystem entirely

2. **Don't use full filesystem as intermediary**
   - FileStore's overhead: filesystem metadata, journaling, fragmentation
   - BlueStore's direct block access eliminated this completely

3. **Don't ignore write amplification**
   - It's the dominant performance killer across all backends
   - Track per-category WAF; small random writes = biggest risk
   - Batch small writes instead of writing individually

4. **Don't assume one device type**
   - Ceph started HDD-only, had to add SSD/NVMe support later
   - Designed device hierarchy from the start, but colocation logic evolved
   - Support SSD/HDD mixing from day one

5. **Don't use thread-per-operation at scale**
   - Classic OSD thread contention limited to ~16 cores
   - Crimson's sharded/seastar model enables 64+ core scaling
   - Design for partitioned/concurrent execution from start

6. **Don't make crash recovery an afterthought**
   - WAL protocol must be designed from first write
   - Replay-on-restart must be explicit, not magical
   - `committed_to` tracking must be integrated, not bolted on

7. **Don't optimize only for average latency**
   - FileStore great average latency but terrible 99.99th tail (7.3s vs 334ms)
   - BlueStore provides good average AND tail latency on HDDs
   - Design for tail latency, not just average

---

## 10. Performance Optimization Patterns (From Real Data)

### From "New in Luminous: BlueStore" (Ceph Blog 2017)

**Write performance gains:**
- ~2x faster writes vs FileStore (avoid double-write for large writes)
- 3x faster for RGW small random writes
- Better tail latency even on HDDs (334ms 99.99th vs FileStore's 7.3s)
- No throughput collapse when filling cluster (FileStore's "splitting" problem)

**Small write advantages:**
- Significantly better than FileStore even with journal
- Avoid "very ugly behavior" in FileStore key/value data
- For some RGW workloads: 3x write performance improvement

**Snapshot/copy-on-write benefits:**
- Recent snapshots perform much better (CoW vs overwrite-in-place)
- Snapset management much lighter than FileStore equivalents

### From MSST 2017 "Understanding Write Behaviors of Storage"

**HDD results:**
- No matter which backend: WAF 14.56 to 71.03 (with replication factor 3)
- FileStore best IOPS and average latency
- But 99.99th tail latency terrible: up to 7.3 seconds
- BlueStore: much better 99.99th tail latency: 334.0 ms
- BlueStore larger WAF than FileStore but much better latency

**SSD results:**
- FileStore best across the board (IOPS, average latency, tail latency)
- BlueStore performs a little behind FileStore
- KStore worst (compaction overhead)
- Reason: FileStore fast journal write speed; client ACKs as soon as journal write completes

### Key takeaways for your filesystem

| Metric | Target | Ceph Reference |
|--------|--------|----------------|
| **Write amplification** | < 10x (without replication) | BlueStore on HDDs: ~13-66x; on SSDs: better |
| **99.99th tail latency** | < 500 ms (HDD), < 50 ms (SSD) | BlueStore HDD: 334ms; FileStore HDD: 7300ms |
| **Throughput scalability** | Linear to 32+ cores | Crimson Seastar; classic Ceph ~16 cores |
| **Small write performance** | < 1ms latency | BlueStore WAL only for writes < min_alloc_size |
| **Snapshot performance** | No degradation recently | BlueStore CoW advantage |

---

## Summary: Your Filesystem Learning Checklist

### Must-learn (foundational, order matters)
- [ ] **Bypass filesystem layer** — direct raw block access, no FUSE/filesystem intermediary
- [ ] **Embed KV store for metadata** — RocksDB or equivalent (don't build B-tree from scratch)
- [ ] **Three-device hierarchy** — data/DB/WAL with auto-colocation logic
- [ ] **Selective journaling** — only small writes; large data bypasses journal
- [ ] **Write amplification tracking** — classify writes, track WAF per category

### Should-learn (important for performance)
- [ ] **Explicit crash recovery** — WAL → acknowledge → commit → clear sequence
- [ ] **Multi-core scalability** — shard/PG partitioning, minimize cross-core communication
- [ ] **Configurable thresholds** — min_alloc_size, WAL size, batch capacities per device type
- [ ] **Cache hierarchy** — subdivide into metadata/KV/data with configurable ratios
- [ ] **Tail latency optimization** — design for 99.99th percentile, not just average

### Nice-to-learn (advanced, workload-dependent)
- [ ] **Batch flushing** — accumulate small writes, flush in batches
- [ ] **Copy-on-write for snapshots** — better snapshot performance, space efficiency
- [ ] **Device-tier optimization** — different allocation sizes for HDD vs SSD vs NVMe
- [ ] **Advanced compaction** — leveled vs FIFO vs time-window compaction strategies
- [ ] **Zone/Namespace support** — ZNS device integration for flash-friendly writes

### Anti-patterns to avoid (learn from Ceph's 10+ years)
- [ ] No filesystem layering on top of raw devices
- [ ] No "journaling of journal" designs
- [ ] No thread-per-op at scale (>16 cores)
- [ ] No ignoring write amplification
- [ ] No assuming single device type
- [ ] No average-latency-only optimization
- [ ] No post-hoc crash recovery design

---

## Quick Reference: Key Ceph Config Options to Replicate

| Ceph Option | Category | Your Equivalent |
|-------------|----------|-----------------|
| `block` | Device path | Primary data device |
| `block.db` | Device path | Metadata/DB device (SSD preferred) |
| `block.wal` | Device path | WAL device or auto-colocated with DB |
| `bluestore_min_alloc_size` | Allocation | Minimum write unit (HDD: 4KiB, SSD: 512B default) |
| `bluestore_block_wal_size` | WAL size | Write-ahead log capacity (512MB-4GB typical) |
| `bluestore_sync_submit_transaction` | Safety | Sync metadata on every commit (true/false) |
| `bluestore_prefer_deferred_size` | Deferred writes | Disable for safety (0), enable for performance (>0) |
| `bluestore_cache_size_ssd` | Cache size | Metadata+KV+data cache for SSDs (8GB typical) |
| `bluestore_cache_meta_ratio` | Cache ratio | Fraction of cache for metadata (0.5 typical) |
| `seastore_journal_batch_capacity` | Batch size | Crimson: journal flush threshold |
| `seastore_journal_batch_flush_size` | Batch flush | Crimson: size threshold for flush |

---

**Final thought**: The single most important decision from Ceph's evolution is **blueStore's choice to bypass the filesystem layer entirely**. This one decision eliminated the "journaling of journal" problem, reduced write amplification, improved both average and tail latency, and enabled 2x write performance gains. Everything else — device hierarchy, journaling, crash recovery, multi-core scalability — flows from that foundation. Start there, and the rest becomes significantly easier to design and optimize.

---