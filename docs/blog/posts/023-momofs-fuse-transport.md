---
title: 'R4: momofs FUSE Transport — Mounting Momo as a Filesystem'
date: 2026-08-28 15:57:34+00:00
draft: false
post_type: architecture
tags:
- go
- momofs
- fuse
- posix
- mount
categories:
- momofs
summary: 'The FUSE transport (`momo -imp fs`): bazil.org/fuse binding, per-handle
  buffered writes, /dev/fuse-gated e2e, and a clean unmount lifecycle.'
artifacts:
- type: pr
  id: '963'
- type: issue
  id: '962'
- type: spec
  path: openspec/changes/r4-momofs
related:
- 022-momofs-posix-core
- 004-cas-content-addressable-store
- 028-roadmap-and-research
---
We had a filesystem core — directories as content-addressed manifests, files as
blobs, atomic rename, hardlinks. We had no way for a normal program to *use* it.
You cannot `open()` a manifest. The gap between "we can model a POSIX tree" and
"`/mnt/momo/notes.txt` behaves like a file" is the kernel, and closing that gap is
what the FUSE transport does.

## The Real Problem

Linux exposes filesystems through the kernel. A program calls `open`, `read`,
`write`, and `readdir`, and the kernel decides which filesystem answers. Most
filesystems are kernel code — you write a kernel module, and a bug in it can take
down the machine. That is an unreasonable bar for a storage daemon whose whole
point is to be a userspace program.

**FUSE** — Filesystem in Userspace — removes that bar. The kernel provides a
generic bridge: it forwards filesystem calls to a userspace process over a device
file called `/dev/fuse`, and that process implements the filesystem in whatever
language it likes. If the daemon crashes, the kernel marks the mount dead instead
of panicking. The trade is a small cost per call (a round trip to userspace) for
the enormous benefit of not writing kernel code.

So the problem narrowed to: how does a userspace process translate the kernel's
file operations into operations on content-addressed manifests and blobs, and how
do we keep the mount from leaking when the process dies?

## Why the Obvious Solutions Failed

**An S3 gateway is not enough.** The native API and the S3 gateway already let
programs read and write objects, but they speak HTTP, not the `read`/`write`
syscalls a normal program expects. Unmodified tools — `cat`, `cp -r`, an editor —
cannot use an HTTP object API without a client. A filesystem is the universal
interface.

**A custom kernel module.** Writing the filesystem as kernel code would remove the
userspace hop, but it means a memory-safety bug in our storage logic becomes a
kernel panic, and every kernel version becomes a compatibility surface. We chose
the safe boundary.

**Buffering the whole file in memory.** The simplest way to reconcile the kernel's
byte-range write model with our write-whole-file blob model is to hold the entire
file in RAM until close. That is correct but unbounded: a large write would pin
its full size in the daemon's memory, and a few concurrent writers could exhaust
it.

**Caching aggressively for speed.** The mount could answer `getattr` from a cache
for longer, but then a file written through the native API would stay invisible in
the mount for an arbitrary window. We wanted read-your-own-writes to hold.

## The Solution: A FUSE Transport Over the Core

The R4 FUSE transport exposes the POSIX core to the kernel through FUSE, using the
`bazil.org/fuse` binding (the Go library that speaks the FUSE protocol). Tracking
issue #962 led to pull request #963. The daemon gains a new impersonation mode:
instead of acting as a server, it mounts a store as a filesystem and serves
kernel calls until told to stop.

{{< diagram src="/diagrams/09-fuse-mount-stack.svg" alt="momofs FUSE transport stack" caption="Figure 1: Architectural layers bridging POSIX filesystem calls to the immutable CAS store" >}}

The pieces:

- **Mount entrypoint.** Running the daemon in filesystem mode (`momo -imp fs`)
  with a mount point and an optional data directory starts a FUSE server. The
  server is context-cancellable, and on `SIGINT` or `SIGTERM` the process unmounts
  cleanly before exiting.
- **The FUSE adapter.** A set of node types model the tree to the kernel: a root,
  directories, files, and per-open handles for files and directories. Every error
  the core can return is mapped to the matching POSIX error code (the `errno` the
  kernel expects) so that a missing file returns "not found" rather than a generic
  failure.
- **Writes buffer per handle, not per file.** The kernel writes files in arbitrary
  byte ranges; our CAS layer wants a whole blob. The adapter keeps a bounded
  buffer per open handle and **materializes a single whole blob on flush or
  release.** Reads go the other way and stream from the core.
- **Testing that respects the environment.** Node and handle behavior is covered
  by unit tests, and a kernel round-trip end-to-end test runs against a real
  `/dev/fuse` mount — but self-skips on machines (such as most CI runners) where
  `/dev/fuse` is absent. Goroutine-leak and race-detector checks accompany it.

```go
// The kernel hands us byte-range writes; the CAS layer stores whole immutable
// blobs. We reconcile the two by buffering per open handle and materializing one
// blob at flush/release. The buffer is bounded, so a large write costs a bounded
// amount of daemon memory rather than the whole file.
func (h *fuseFileHandle) Write(ctx context.Context, req *fuse.WriteRequest,
    resp *fuse.WriteResponse) error {
    // Grow within a cap; refuse rather than pin unbounded memory.
    if len(h.buf)+len(req.Data) > maxHandleBuffer {
        return syscall.ENOSPC
    }
    h.buf = append(h.buf, req.Data...)
    resp.Size = len(req.Data) // the kernel believes the range landed
    return nil
}
```

## Implemented vs. Planned (Honesty Note)

The POSIX write model and metadata semantics described here are **implemented** —
the core plus the transport plus the mount user guide all agree. What is still
*planned* is finer-grained write behavior: full `mmap` byte-range correctness and
POSIX locks. Today, writes are buffered per handle and materialized whole on
flush; truncation size hints are accepted but their semantics land with the
byte-range follow-up.

## ⚡ Bolt + 🛡 Sentinel Lens

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- **⚡ Bolt**: per-handle write buffers are bounded, so memory does not grow without
  limit during large in-flight writes; reads stream rather than copy the blob into
  daemon memory.
- **🛡 Sentinel**: a clean unmount is a safety property, not housekeeping. A stale
  FUSE mount left behind by a crash presents a partial tree, so shutdown unmounts
  deliberately; scatter operations stay lease-validating so rename and refcount
  survive across the transport.

## Failure Modes

| Risk | Guard |
|------|-------|
| Daemon crash leaves a dead mount | Kernel marks the mount dead; shutdown path unmounts on signal |
| Large write overwhelms daemon memory | Per-handle buffer is bounded and refuses beyond the cap |
| Native write invisible in mount | Manifests are read fresh, so reads are read-your-own-writes |
| Core error surfaces as generic failure | Errors are mapped to precise POSIX codes |
| CI lacks `/dev/fuse` | Kernel e2e self-skips; unit tests still run |

## When NOT to Use This

- **Non-Linux hosts.** FUSE here targets Linux `/dev/fuse`; macOS and Windows use
  different transports (see the migration post and the roadmap).
- **Pure object workloads.** If no program needs the mount, the FUSE hop is pure
  overhead — use the native or S3 interface instead.
- **Write-heavy large appends today.** Whole-blob materialization makes large
  append workloads pay whole-file cost until the byte-range follow-up lands.

## References / Dig deeper

- Spec: `openspec/changes/r4-momofs`.
- Pull request #963; tracking issue #962.
- The core underneath: [022: momofs POSIX Core](022-momofs-posix-core.md).
- Byte layer: [004: Content-Addressable Storage](004-cas-content-addressable-store.md).
- Where R4 sits: [028: Roadmap and Research](028-roadmap-and-research.md).
- Later migration to go-fuse/v2: [029: Migrating to go-fuse/v2](029-fuse-go-fuse-v2-migration.md).
- Operational docs: `docs/momofs/MOUNT_USER_GUIDE.md`,
  `docs/momofs/IMPLEMENTATION.md`.
