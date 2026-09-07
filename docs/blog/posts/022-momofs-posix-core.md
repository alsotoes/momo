---
title: "R4: momofs — POSIX Core Over the CAS Store"
date: 2026-08-27T22:40:31Z
draft: false
tags: [go, momofs, posix, cas, storage]
categories: [momofs]
summary: "The R4 POSIX core: inodes as content-addressed manifests, files as blobs, atomic rename, hardlinks, permission enforcement — a filesystem on momo."
artifacts:
  - {type: pr, id: "957"}
  - {type: issue, id: "932"}
  - {type: issue, id: "962"}
  - {type: spec, path: openspec/changes/r4-momofs}
related:
  - 004-cas-content-addressable-store
  - 023-momofs-fuse-transport
  - 005-crush-placement
---
# R4: momofs — POSIX Core Over the CAS Store

The biggest architecture bet yet: make momo a **filesystem**, not just an
object store. R4 ships the POSIX core (#957), tracked by issue #932.

## The core idea

Reuse the CAS store ([004](004-cas-content-addressable-store.md)) as the byte
layer and add a **metadata layer**:

- **Directories** = content-addressed *manifests* (name → entry lists stored as
  objects).
- **Files** = the existing blobs themselves (hash-addressed, dedup).
- **Inode metadata** (mode/uid/gid) rides alongside each manifest/blob.

So `mkdir / x / file.txt` is a manifest o + a blob: **one store, visible as
both S3/native key space and a POSIX tree** — consistency across physical
interfaces for free (single manifest source).

## What came with it

- Lookup/getattr/setattr/readdir/open/create/read/write against the CAS layer.
- **Atomic rename + hardlinks** with refcounts aligned to the GC/tombstone
  sweep — POSIX rename semantics *and* content-addressable integrity.

## Implemented-state note (Rule 76)

The `docs/momofs/` suite is largely a **design/plan** corpus. This journal's
factual layer is `src/momofs` (implemented in #957) and operational docs kept
in sync (`docs/momofs/MOUNT_USER_GUIDE.md`, `IMPLEMENTATION.md §2.3`). Design
features not yet implemented — e.g. full POSIX ACLs, quotas, snapshots — remain
*planned*, not shipped.

## ⚡ Bolt + Sentinel lens

- **Bolt**: dense zero-allocation metadata handling (manifests are small, hot,
  and read-heavy).
- **Sentinel/GC**: refcount/GC alignment so a hardlink count doesn't permit the
  garbage collector to reap a live blob (tombstone sweep — [007](007-at-rest-integrity-and-gc.md)).

## Related

Byte layer: [004](004-cas-content-addressable-store.md). Mount with FUSE:
[023](023-momofs-fuse-transport.md).