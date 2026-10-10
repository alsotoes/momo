# Specification: Adaptive Volume Storage Seam with O(1) Disk Access and Hole-Punching

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1128

## Purpose

This specification defines the architecture, binary layout, and runtime contracts for Momo's `VolumeBlobStore` and `AdaptiveBlobStore` seams. It establishes $O(1)$ single-seek disk access for small and medium objects, host filesystem inode conservation, sparse hole-punching deletion, instant crash recovery, and adaptive polymorphic routing.

## ADDED Requirements

### Requirement: Needle Framing and 8-byte Alignment

The `VolumeBlobStore` SHALL store objects as sequential binary needles inside volume superblocks (`vol-XXXX.dat`). Each needle SHALL be padded to an 8-byte boundary and MUST begin with a 4-byte magic sequence (`0x4D4F4D4F`), followed by a 4-byte payload length, a 32-byte content hash, a 2-byte flag header, the raw payload bytes, and a 4-byte CRC32C checksum trailer.

#### Scenario: Needle append with 8-byte alignment
- **GIVEN** a valid payload of length 1,025 bytes with SHA-256 hash `H`
- **WHEN** `PutBlob(H, reader)` is invoked on `VolumeBlobStore`
- **THEN** the needle is appended to the active volume file with magic `0x4D4F4D4F`, 4-byte length `1025`, 32-byte hash `H`, payload, and CRC32C trailer, padded with null bytes so the next needle begins on an 8-byte boundary.

#### Scenario: Corrupted needle payload detected
- **GIVEN** a needle stored in a volume whose payload bytes are tampered or bit-rotted
- **WHEN** `GetBlob(H)` is called
- **THEN** the CRC32C or SHA-256 validation fails, returning `syscall.EBADMSG` and closing the stream immediately.

---

### Requirement: O(1) Single-Seek Read Execution

The `VolumeBlobStore` SHALL execute read requests via a single `pread` syscall using the recorded `(VolumeID, Offset, Size)` locator stored in Bbolt metadata, completely bypassing filesystem directory path resolution and inode lookups.

#### Scenario: Direct needle seek
- **GIVEN** a committed needle located at Volume `1`, offset `65536`, size `4096`
- **WHEN** `GetBlob(H)` is invoked
- **THEN** the store seeks directly to offset `65536` on the open volume file descriptor in a single read operation, returning an `io.ReadCloser` over the needle payload without traversing directory trees.

---

### Requirement: Sparse Deletion via Linux Hole Punching

Upon deletion of a needle, the storage engine SHALL issue a Linux `fallocate` syscall with `FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE` covering the needle's physical byte range, releasing the underlying filesystem disk blocks back to the OS free pool immediately without shifting file offsets or rewriting the volume file.

#### Scenario: Immediate block deallocation on deletion
- **GIVEN** a needle stored in an ext4 or XFS volume file occupying 16 filesystem blocks
- **WHEN** `DeleteBlob(H)` is called
- **THEN** `fallocate` deallocates the blocks, reducing the file's allocated block count (`stat.Blocks`) while preserving the file's logical size and neighboring needle offsets.

---

### Requirement: Atomic Bbolt-Anchored Tail Truncate Recovery

A needle write SHALL be considered committed only after its volume offset and length are synchronized in Bbolt. On startup, the `VolumeBlobStore` SHALL compare the physical file size of the active volume against the maximum committed offset in Bbolt. If uncommitted partial bytes exist at the tail, it SHALL truncate the file back to the committed offset.

#### Scenario: Uncommitted partial tail truncation on startup
- **GIVEN** a server crash during an append that left 512 uncommitted bytes past the last Bbolt-recorded offset `1048576`
- **WHEN** the `VolumeBlobStore` initializes upon reboot
- **THEN** it executes `volumeFile.Truncate(1048576)`, restoring the active volume to a pristine committed state in $O(1)$ time without scanning or reparsing historical needles.

---

### Requirement: Adaptive Polymorphic Routing Seam

The `AdaptiveBlobStore` SHALL implement the `BlobStore` interface and dynamically route objects between `VolumeBlobStore` and `LocalBlobStore` based on a configurable size threshold (default 1 MiB).

#### Scenario: Small object routed to volume needle
- **GIVEN** an object with size &le; 1 MiB
- **WHEN** `PutBlob` is executed through the `AdaptiveBlobStore`
- **THEN** it is delegated to `VolumeBlobStore` and appended as a needle.

#### Scenario: Large object routed to direct CAS file
- **GIVEN** an object with size > 1 MiB
- **WHEN** `PutBlob` is executed through the `AdaptiveBlobStore`
- **THEN** it is delegated to `LocalBlobStore`, written as a standalone CAS file, and benefits from direct OS page cache readahead and zero volume churn.

---

### Requirement: Sealed Volume Immutability

When an active volume file exceeds the configured maximum volume size (default 1 GiB), it SHALL transition to `Sealed` status. Sealed volumes SHALL be opened in read-only mode (`O_RDONLY`), and all subsequent writes SHALL be directed to a newly created active volume file.

#### Scenario: Automatic volume roll on threshold
- **GIVEN** an active volume `vol-0001.dat` at 1,073,741,000 bytes
- **WHEN** an append pushes the volume size past the 1 GiB threshold
- **THEN** `vol-0001.dat` is marked sealed and closed for writing, and `vol-0002.dat` is opened as the active volume.
