# Proposal: Adaptive Volume Storage Seam with O(1) Disk Access and Hole-Punching (SeaweedFS Pattern)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1128

- **Champion:** @alsotoes
- **Status:** `Proposed`

## 1. Problem

Momo's storage engine (`src/storage/local_blobstore.go`) maps every CAS object directly to an individual file on the host filesystem (`<base>/blobs/ab/cd/ef/<full-hash>`).

While this 1-to-1 mapping is well-suited for large files (> 5 MiB), it encounters fundamental scalability bottlenecks when handling workloads composed of millions or billions of small to medium objects (e.g., IoT telemetry, images, JSON configurations, POSIX files via MomoFS):

1. **Host Inode Exhaustion:** 10,000,000 small files consume 10,000,000 filesystem inodes, rapidly exhausting inode tables on Linux `ext4`/`xfs` filesystems even when substantial raw disk capacity remains.
2. **Random Metadata Seek Penalties ($O(k)$ Read Complexity):** Every read of a small object requires 3 to 4 sequential disk I/O operations by the OS filesystem (reading directory inode → reading directory data block → reading file inode → reading data block). Under high concurrent load, this generates severe disk head thrashing and read latency spikes.
3. **Write Amplification & Syscall Overhead:** Every upload of a small object triggers `os.CreateTemp()`, directory existence checks (`os.MkdirAll`), buffer writes, directory atomic `os.Rename()`, and directory metadata `fsync`, creating high syscall and context-switch churn.
4. **Cloud Tiering Inefficiency:** Backing up or archiving millions of small files to AWS S3 via `S3BlobStore` incurs prohibitive API costs ($0.005 per 1,000 PUT requests = $5,000 per 1B files) and triggers aggressive HTTP throttling.

## 2. Proposed Solution

Inspired by SeaweedFS and Facebook's *Needle in a Haystack* architecture (OSDI '10), we introduce an **Adaptive Volume Storage Seam** (`VolumeBlobStore`) for Momo. Small objects are concatenated as binary "needles" into large, pre-allocated volume superblocks (`vol-0001.dat`), enabling true **$O(1)$ single-seek reads** and **host inode conservation**.

Critically, we architect this system to **eliminate the historical drawbacks of SeaweedFS**:

### 2.1 The 5 Architectural Innovations Eliminating SeaweedFS Cons

1. **Zero Vacuuming Overhead via Linux Kernel Hole-Punching (`fallocate`):**
   - *SeaweedFS Drawback:* Deleting needles leaves dead space, requiring an expensive background compaction engine (vacuuming) that continuously copies active needles to new files, burning disk I/O and CPU.
   - *Momo Solution:* On deletion, Momo invokes Linux `fallocate(fd, FALLOC_FL_PUNCH_HOLE|FALLOC_FL_KEEP_SIZE, offset, length)` on the volume file. The Linux kernel immediately deallocates the underlying physical disk blocks and returns them to the filesystem free pool without shifting file offsets. Compaction I/O and vacuum background churn are **100% eliminated**.

2. **Zero Extra RAM Index Footprint (Bbolt Colocation):**
   - *SeaweedFS Drawback:* SeaweedFS maintains an in-memory `.idx` table in RAM (`NeedleID -> Offset, Size`), consuming 4–6 GB of RAM for 100M files.
   - *Momo Solution:* Momo already uses Bbolt (`bbolt.DB`) in `CASStore` for file metadata (`bucketFiles` and `bucketHashes`). We embed the volume locator directly into the existing metadata entry (`VolumeID uint32` + `Offset uint32` = 8 bytes). The Linux kernel automatically pages Bbolt pages via `mmap`. Index memory footprint in user-space RAM is **zero**.

3. **Instant $O(1)$ Crash Recovery via Bbolt-Anchored Tail Truncate:**
   - *SeaweedFS Drawback:* Abrupt crashes can corrupt the volume tail, requiring a full scan and parse of the volume file on boot.
   - *Momo Solution:* A needle append is only committed when its offset is synced in Bbolt. On boot, Momo compares the physical volume file size with the last committed offset in Bbolt. If uncommitted partial bytes exist at the tail, a single `Truncate(committedOffset)` syscall restores clean state instantly. Startup recovery is **$O(1)$ and executes in < 1 millisecond**.

4. **Polymorphic Storage Seam (Rule 74):**
   - *SeaweedFS Drawback:* Forcing large files (> 50 MiB) into volume files causes fragmentation and heavy volume thrashing.
   - *Momo Solution:* An `AdaptiveBlobStore` seam inspects payload size:
     - Objects &le; `Threshold` (default **1 MiB**) route to `VolumeBlobStore` (needle packing, 0 inodes, $O(1)$ single seek).
     - Objects > `Threshold` route to `LocalBlobStore` (direct CAS files, zero-copy streaming, OS readahead).

5. **Decentralized Placement via CRUSH (Zero Master Servers):**
   - *SeaweedFS Drawback:* SeaweedFS relies on a centralized Master server cluster with Raft consensus to assign Volume IDs.
   - *Momo Solution:* In strict adherence to **Rule 2 (Decentralized Primary)** and **Rule 12 (CRUSH Placement)**, Momo assigns Volume IDs algorithmically using CRUSH:
     $$\text{VolumeID} = \text{CRUSH}(\text{Hash}, \text{ClusterMap})$$
     Zero central coordinators, zero single points of failure.

## 3. Storage & Binary Layout

### 3.1 Needle Binary Framing
Needles in volume superblocks (`vol-XXXX.dat`) are 8-byte aligned and structured as:
```
┌──────────────┬──────────────┬──────────────┬──────────────┬──────────────────┬──────────────┐
│ Magic (4B)   │ PayloadLen   │ Hash (32B)   │ Flags (2B)   │ Payload (N B)    │ CRC32C (4B)  │
│ 0x4D4F4D4F   │ (4B)         │ SHA-256      │ Tombstone/etc│ Raw blob content │ Checksum     │
└──────────────┴──────────────┴──────────────┴──────────────┴──────────────────┴──────────────┘
```
- **Magic:** `0x4D4F4D4F` (`"MOMO"` in ASCII) identifies a valid needle boundary.
- **Integrity:** CRC32C trailing checksum + SHA-256 header hash ensure verify-on-read compliance (Rule 74).

### 3.2 Volume Sizing
- Default volume size: **1 GiB** (configurable up to 32 GiB).
- When a volume reaches its capacity, it transitions to `Sealed` (read-only), and a new active volume is opened.

## 4. Testing & Verification Plan

1. **Needle Append & $O(1)$ Read Verification:** Write 100,000 small blobs (1KB–64KB); assert that reads execute via a single `pread` syscall without directory traversal.
2. **Hole-Punch Deletion Verification:** Delete needles; assert using `stat.Blocks` that physical filesystem blocks are immediately reclaimed by the OS free pool.
3. **Crash & Truncation Safety:** Simulate uncommitted partial tail writes; verify startup recovery truncates back to the clean committed offset.
4. **Adaptive Seam Delegation:** Verify objects &le; 1 MiB land in `VolumeBlobStore` and objects > 1 MiB land in `LocalBlobStore`.
5. **Full Suite Regression:** Pass `make test` with race detection (`go test -race`) and `goleak` verification.

## 5. Backward Compatibility & Standards Alignment

- **Interface Compatibility:** `VolumeBlobStore` strictly implements the existing `storage.BlobStore` interface (`src/storage/blobstore.go`). Existing `CASStore` methods (`Put`, `Get`, `GetMeta`, `Delete`) remain unchanged.
- **Protocol Stability (Rule 7/33):** Wire protocols (`momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`) remain 100% untouched.
- **POSIX Error Mapping (Rule 10):** All storage errors map to POSIX syscall constants (`syscall.ENOENT`, `syscall.EBADMSG`, `syscall.EIO`, `syscall.ENOSPC`).
- **Unified Panic Recovery (Rule 37):** All methods include two-line `defer recover()` blocks returning formatted POSIX errors.
