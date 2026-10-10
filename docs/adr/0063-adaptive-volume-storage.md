# 0063-adaptive-volume-storage

## Status
Accepted

## Confidence
High

## Context
Momo's storage engine (`src/storage/local_blobstore.go`) maps every CAS object directly to an individual file on the host filesystem (`<base>/blobs/ab/cd/ef/<full-hash>`).

While this 1-to-1 mapping is well-suited for large files (> 5 MiB), it encounters fundamental scalability bottlenecks when handling workloads composed of millions or billions of small to medium objects (e.g., IoT telemetry, images, JSON configurations, POSIX files via MomoFS):

1. **Host Inode Exhaustion:** 10,000,000 small files consume 10,000,000 filesystem inodes, rapidly exhausting inode tables on Linux `ext4`/`xfs` filesystems even when substantial raw disk capacity remains.
2. **Random Metadata Seek Penalties ($O(k)$ Read Complexity):** Every read of a small object requires 3 to 4 sequential disk I/O operations by the OS filesystem (reading directory inode → reading directory data block → reading file inode → reading data block). Under high concurrent load, this generates severe disk head thrashing and read latency spikes.
3. **Write Amplification & Syscall Overhead:** Every upload of a small object triggers `os.CreateTemp()`, directory existence checks (`os.MkdirAll`), buffer writes, directory atomic `os.Rename()`, and directory metadata `fsync`, creating high syscall and context-switch churn.
4. **Cloud Tiering Inefficiency:** Backing up or archiving millions of small files to AWS S3 via `S3BlobStore` incurs prohibitive API costs ($0.005 per 1,000 PUT requests = $5,000 per 1B files) and triggers aggressive HTTP throttling.

## Decision
- Needle Framing and 8-byte Alignment: The `VolumeBlobStore` SHALL store objects as sequential binary needles inside volume superblocks (`vol-XXXX.dat`). Each needle SHALL be padded to an 8-byte boundary and MUST begin with a 4-byte magic sequence (`0x4D4F4D4F`), followed by a 4-byte payload length, a 32-byte content hash, a 2-byte flag header, the raw payload bytes, and a 4-byte CRC32C checksum trailer. ---
- O(1) Single-Seek Read Execution: The `VolumeBlobStore` SHALL execute read requests via a single `pread` syscall using the recorded `(VolumeID, Offset, Size)` locator stored in Bbolt metadata, completely bypassing filesystem directory path resolution and inode lookups. ---
- Sparse Deletion via Linux Hole Punching: Upon deletion of a needle, the storage engine SHALL issue a Linux `fallocate` syscall with `FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE` covering the needle's physical byte range, releasing the underlying filesystem disk blocks back to the OS free pool immediately without shifting file offsets or rewriting the volume file. ---
- Atomic Bbolt-Anchored Tail Truncate Recovery: A needle write SHALL be considered committed only after its volume offset and length are synchronized in Bbolt. On startup, the `VolumeBlobStore` SHALL compare the physical file size of the active volume against the maximum committed offset in Bbolt. If uncommitted partial bytes exist at the tail, it SHALL truncate the file back to the committed offset. ---
- Adaptive Polymorphic Routing Seam: The `AdaptiveBlobStore` SHALL implement the `BlobStore` interface and dynamically route objects between `VolumeBlobStore` and `LocalBlobStore` based on a configurable size threshold (default 1 MiB). ---
- Sealed Volume Immutability: When an active volume file exceeds the configured maximum volume size (default 1 GiB), it SHALL transition to `Sealed` status. Sealed volumes SHALL be opened in read-only mode (`O_RDONLY`), and all subsequent writes SHALL be directed to a newly created active volume file.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/055-adaptive-volume-storage.md

## References
- Issue: #1128
- PR: 
- Spec: `openspec/changes/adaptive-volume-storage/`
- Blog: docs/blog/posts/055-adaptive-volume-storage.md

