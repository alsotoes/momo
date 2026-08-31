# OSD Journaling Mechanisms: BlueStore vs Crimson

## BlueStore Journaling (Ceph Classic)

### WAL Device Mechanism
- **Write-ahead log** (`block.wal`) stores internal journal for consistency
- WAL ensures no acknowledged write is lost on OSD crash
- **Sequence**: Write data → Acknowledge → Commit to final location → Remove WAL entry
- **Crash recovery**: WAL replays on restart to complete committed writes
- **Placement**: On fastest device (SSD/NVDIMM) or colocated with DB
- **Sizing**: 512 MB to 4 GB per OSD typically sufficient
- **Config**: `bluestore_block_wal_size`, `bluestore_prefer_deferred_size`, `bluestore_sync_submit_transaction`

### Key Properties
- **Metadata consistency**: Only journals small writes < `min_alloc_size` (64 KiB default)
- **No file system overhead**: Data written directly to raw block devices
- **Two-device option**: Separate WAL device for improved performance
- **Default**: Colocated with data/DB on single device

### WAL Lifecycle
1. Client write received by OSD
2. Data written to WAL (journal device or main device)
3. Acknowledge to client
4. Data committed to final location on main block device
5. WAL entry removed after successful commit
6. On crash between steps 2-3: WAL replays on restart

---

## Crimson Journaling (SeaStore/RBM)

### CircularBoundedJournal
- **Fixed-size journal** (`seastore_cbjournal_size` config)
- **Record-based**: Stores `record_t` with deltas and extents
- **Two implementations**:
  - `SegmentedJournal`: For segmented devices, one open segment as journal
  - `CircularBoundedJournal`: For RBM devices, fixed-size circular buffer
- **Journal types**:
  - `JOURNAL_SEQ_NULL` = initial state
  - Records contain deltas for extent modifications
  - `committed_to` tracks last committed epoch/sequence

### Key Properties
- **Crash recovery**: Replay journal deltas to reconstruct state
- **Batch flushing**: Configurable via `seastore_journal_batch_capacity`, `seastore_journal_iodepth_limit`
- **Inline vs OOL**: Small extents inline; large extents written out-of-line (OOL segments)
- **No "splay" replication**: Term `splay` only in `journal-splay-width` config (journal metadata width)

### Crimson Journal Lifecycle
1. Transaction begins (`do_transaction`)
2. `SUBMIT_JOURNAL` stage: `journal->submit_record()`
3. Record written to circular buffer
4. Batch flush when capacity threshold reached or size threshold
5. On crash: Replay from `written_to` to `alloc_tail` to reconstruct state
6. `record_submitter.update_committed_to()` updates committed state

---

## Comparison Table

| Aspect | BlueStore | Crimson |
|--------|-----------|---------|
| **Journal type** | WAL device (write-ahead log) | CircularBoundedJournal / SegmentedJournal |
| **Storage** | Separate device or colocated | Fixed-size circular buffer |
| **Record format** | Internal KV operations | `record_t` with deltas + extents |
| **Crash recovery** | WAL replay on restart | Delta replay from journal records |
| **Config options** | `bluestore_block_wal_size`, `bluestore_db_size` | `seastore_journal_batch_capacity`, `seastore_cbjournal_size` |
| **Placement** | Fastest device (SSD/NVDIMM) | Fixed device, configured size |
| **Inline extents** | N/A (data on main device) | Small extents inline; large → OOL segments |
| **Batch control** | `min_alloc_size`, deferred writes | `seastore_journal_batch_capacity`, `seastore_journal_batch_flush_size` |
| **Purpose** | Metadata consistency + small write handling | Crash recovery + transaction logging |

---

## Theory: Which is the Best Approach?

### BlueStore WAL Advantages
1. **Proven over 15+ years**: Battle-tested in production Ceph clusters
2. **Flexible placement**: Can colocate with data/DB or use separate fast device
3. **Simple semantics**: Write → ACK → Commit → Clear; replay on restart
4. **Performance tunable**: WAL size, deferred writes, sync settings control tradeoff
5. **No double-write**: Data written directly to raw device; only small writes go through WAL

### Crimson Journal Advantages
1. **Modern design**: Built for fast NVMe/SSD devices with Seastar framework
2. **Batch-oriented**: Configurable batch capacity and flush thresholds
3. **Extent tracking**: Explicit delta/extent model for recovery
4. **Segmented approach**: Separate journal segment from data segments
5. **Integration with RMW pipeline**: Journal state ties into replication completion (`pending_commits`)

### Theoretical Recommendation

**For HDD/traditional storage**: **BlueStore WAL** — colocated design minimizes device count, proven crash recovery, minimal overhead for typical workloads.

**For NVMe/fast SSD storage**: **Crimson journal** — batch-oriented design leverages device parallelism; fixed-size journal simplifies recovery; extent model integrates well with RMW pipeline.

**Hybrid approach**: Use BlueStore WAL for metadata consistency + Crimson journal for replication tracking. Both provide equivalent data durability; choice depends on storage device type and performance goals.

**Verdict**: BlueStore WAL for maximum compatibility and proven resilience; Crimson journal for high-performance NVMe workloads where batch processing and extent tracking matter.

---

## Research Papers & Substantiation

### 1. "File Systems Unfit as Distributed Storage Backends: Lessons from 10 Years of Ceph Evolution"
- **Author**: Aghayev, Weil, Kuchnik, Nelson, Ganger, Amvrosiadis
- **Key findings**: 
  - BlueStore avoids "journaling of journal" problem present in FileStore
  - BlueStore WAL reduces write amplification vs FileStore's external journal
  - Small writes still logged in RocksDB WAL (similar to Crimson journal)
  - BlueStore shows better 99.99th tail latency on HDDs (334 ms vs FileStore's 7.3s)
- **PDF**: https://gwern.net/doc/cs/end-to-end-principle/2019-aghayev.pdf

### 2. "Understanding Write Behaviors of Storage" (MSST 2017)
- **Key findings**:
  - FileStore triples write traffic due to external Ceph journaling
  - BlueStore has larger WAF than FileStore on SSDs but better tail latency
  - BlueStore WAL dominant source of write amplification (partial overwrites < chunk size)
  - On SSDs, FileStore outperforms BlueStore due to fast journal write speed
- **PDF**: https://msstconference.org/MSST-history/2017/Papers/CephObjectStore.pdf

### 3. "New in Luminous: BlueStore" (Ceph Blog 2017)
- **Key findings**:
  - BlueStore internal journaling much lighter than FileStore journal
  - Only journals small writes when faster/necessary
  - Fast device can store metadata (DB device) instead of just WAL
  - Three-device setup: HDD data + SSD DB + NVDIMM WAL optimal

### 4. Crimson Design Documents (ceph-notes)
- **Key findings**:
  - SeaStore built with Seastar for core-scales performance
  - Journal log-based recovery + backfill core feature
  - Two-phase: write → journal → commit → replication
  - Circular bounded journal for RBM devices; segmented for other devices

### 5. "How to Understand Write-Ahead Logging for Data Safety in BlueStore" (2026)
- **Key findings**:
  - BlueStore WAL guarantees no acknowledged write lost on crash
  - WAL placement on NVMe significantly improves write latency
  - Configurable: disable deferred writes for maximum safety, enable for performance
  - `bluestore_sync_submit_transaction` for metadata sync control

---

## Best Approach Summary

| Workload Type | Recommended Journal | Rationale |
|--------------|-------------------|-----------|
| **HDD traditional** | BlueStore WAL | Colocated design, minimal devices, proven 15yr resilience |
| **NVMe SSD mixed** | BlueStore WAL on fast device + DB | WAL on SSD, DB on same/other device, flexible |
| **All-flash NVMe** | Crimson journal | Batch-oriented, extent model, leverages device parallelism |
| **High-write throughput** | Crimson journal | RMW pipeline integration, `pending_commits` counter, batch flushing |
| **Maximum data safety** | BlueStore WAL + deferred writes disabled | Proven replay-on-restart, explicit ACK→Commit→Clear sequence |
| **Balanced performance/safety** | BlueStore WAL default config | Good default; WAL on fastest device, deferred writes enabled |

**Theory**: For most deployments, **BlueStore WAL with SSD device** provides the best balance of performance, simplicity, and proven resilience. **Crimson journal** excels in NVMe-dominant, high-throughput environments where its batch and extent-model design provides measurable advantages.

---

## Implementation Notes

### BlueStore WAL Configuration
```bash
# Set WAL on separate fast device (e.g., NVMe)
ceph config set osd bluestore_block_wal_path /dev/nvme1n1
ceph config set osd bluestore_block_wal_size 2147483648  # 2GB

# For maximum safety: disable deferred writes
ceph config set osd bluestore_prefer_deferred_size 0

# Sync metadata on every commit (slower, safer)
ceph config set osd bluestore_sync_submit_transaction true

# Default balanced config (recommended)
# WAL colocated; deferred writes enabled; sync on commit
```

### Crimson Journal Configuration
```bash
# Fixed-size journal for RBM devices
ceph config set crimson osd_seastore_cbjournal_size 1073741824  # 1GB

# Batch control
ceph config set crimson osd_seastore_journal_batch_capacity 4096
ceph config set crimson osd_seastore_journal_batch_flush_size 256*1024*1024  # 256MB
ceph config set crimson osd_seastore_journal_iodepth_limit 32
ceph config set crimson osd_seastore_journal_batch_preferred_fullness 80

# Segmented journal (if using segmented devices)
# auto-configured based on device type