---
title: "Packing Small Objects into Superblocks: O(1) Reads Without Per-Blob Inodes"
date: "2026-10-06T02:44:44Z"
draft: false
post_type: architecture
tags:
  - storage
  - performance
  - reliability
  - filesystem
categories:
  - storage
  - performance
summary: "Why millions of small files per directory collapse a filesystem, and how a SeaweedFS-style volume layout fixes it: append-only superblocks, 8-byte-aligned needles with CRC32C frames, hole-punching deletion, and crash recovery by truncation. Teaches: the real cost of per-blob files, why alignment beats variable-length framing, and how to make crash recovery O(1)."
artifacts:
  - type: spec
    path: openspec/changes/adaptive-volume-storage
related: ["044-plugin-seam-architecture", "031-core-integrity-verification", "026-metrics-observability"]
difficulty: "advanced"
pattern: "data-locality"
teaches:
  - "volume superblock layout"
  - "aligned needle framing"
  - "fallocate hole punching"
  - "truncate-based crash recovery"
---

Storing a hundred thousand small objects as individual files is deceptively
expensive. Each file costs an inode, a directory entry, and at least one disk
block regardless of payload size. Under a content-addressed store that keeps
every version of every object, those costs compound until directory lookups —
not disk bandwidth — dominate every read. This post explains the volume
superblock layout momo adopted for small objects and why each design choice
earns its bytes.

## The Real Problem

A content-addressed store names files by their hash: `blobs/ab/cd/ef0123…`.
That layout spreads entries across a directory tree, which keeps directories
short — but the per-file floor remains. On ext4 a 1 KiB object consumes 4 KiB
of blocks plus an inode plus directory metadata: an overhead of 5× or more.
Worse, a cluster writing many small objects pays it in system calls: one open,
one write, one fsync, one rename, one close per object.

The workload that exposes this is telemetry-like: many small, immutable
objects, written once, read many times, deleted when expired. The goal is a
layout where reading any given object costs one seek, and where deletion
actually returns space to the filesystem.

## Why the Obvious Solutions Failed

**More directory sharding.** Splitting the hash across more levels shortens
each directory but multiplies the number of directories and does nothing about
the per-file block/inode floor.

**One big log file, compacted later.** Appending everything to a single file
solves the per-file overhead but makes individual deletion impossible without
rewriting the file, and makes crash recovery a full-file scan.

**Keeping an index in the metadata DB and hoping the OS page cache hides the
rest.** The index is necessary either way; it does nothing for the four extra
system calls per write or the space amplification per read.

## The Solution with Annotated Code

### An 8-byte-aligned needle

Each object is a *needle* inside an append-only volume file: a fixed header,
the payload padded to 8 bytes, and a CRC32C trailer.

```text
| magic(4) | len(4) | sha256(32) | flags(2) | payload | pad | crc32c(4) |
|          |        |            |          length padded up to 8        |
```

The padding is what buys O(1) reads: with every needle's size known at the
offset, reading object N is a single `pread(offset, headerLen)` — no scan, no
variable-length parsing to discover where the next needle starts.

```go
// Alignment keeps every needle a whole number of 8-byte units, so the next
// needle's offset is always computable from the current one.
func alignTo8(n int) int { return (n + NeedleAlignment - 1) &^ (NeedleAlignment - 1) }
```

Both integrity checks ride along in the frame: the SHA-256 in the header
confirms the payload is the object it claims to be, and the CRC32C trailer
detects torn or corrupted frames. A needle that fails either check fails the
read — closed, never returned to the caller.

### Hole-punching deletion

Deleting a needle punches a hole in the file, returning its blocks to the
filesystem without rewriting anything:

```go
unix.Fallocate(int(f.Fd()),
    unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE,
    offset, int64(NeedleOverhead)+int64(alignTo8(size)))
```

On filesystems without hole-punch support the needle is tombstoned instead,
and the read path treats a tombstone as a miss.

### Crash recovery by truncation

The write path commits a needle to the metadata index only after its bytes are
synced. On startup the active volume is truncated to the highest committed
offset, discarding any partial needle a crash left in the tail. Recovery cost
is one stat call, not a scan.

> **Principle:** Recover from a crash by trusting committed state and cutting
> off everything after it. Scanning to *discover* what survived is what makes
> recovery slow; an explicit commit point makes it instant.

## Verification

- **Frame round-trip** writes a needle and reads it back byte-for-byte.
- **Volume rolling** fills a superblock and proves the store continues into a
  fresh one without losing any object.
- **Hole-punching** deletes a blob and checks the file's block count via
  `fstat` drops.
- **Crash recovery** kills the store mid-life and reopens it, asserting every
  committed object is intact.
- **Routing** proves the adaptive seam sends small objects to the volume store
  and large ones to the per-blob fallback.

On the reference machine, a 64 KiB needle reads at ~1.7 GB/s single-threaded —
the O(1) seek the layout exists for.

## Failure Modes

- **A torn final needle** is discarded by truncation; a torn *middle* needle
  is caught by the CRC/SHA checks and surfaces as a read error, where the
  self-heal layer can repair it from a replica.
- **Filesystems without hole-punch support** fall back to tombstones, which
  reclaim no space until the volume is rewritten. Vacuum is a separate,
  deferred task.
- **Very large objects** never belong in the layout; the adaptive seam routes
  them to per-blob storage so a single object cannot dominate a superblock.

## When NOT to Use

If objects are large and few, a volume layout adds indirection for nothing —
the per-blob local layout is simpler and just as fast. The layout pays off
specifically where objects are small and numerous relative to the block size.

## References / Dig deeper

- Adaptive volume storage specification: `openspec/changes/adaptive-volume-storage`
- Needle framing: `src/storage/volume_needle.go`
- Volume store, deletion, crash recovery: `src/storage/volume_blobstore.go`
- Adaptive seam: `src/storage/adaptive_blobstore.go`
- Tracking issue: https://github.com/alsotoes/momo/issues/1128
