---
title: 'R4: momofs FUSE Transport — Mounting Momo as a Filesystem'
date: 2026-08-28 15:57:34+00:00
draft: false
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
 R4: momofs FUSE Transport — Mounting Momo as a Filesystem

Issue #962 → PR #963: `momo -imp fs` exposes the CAS-store-backed POSIX core
([022](022-momofs-posix-core.md)) to the **kernel** via FUSE, using the pinned
`bazil.org/fuse` binding (v0.0.0-20230120002735).

## What shipped

- **Mount entrypoint**: `momo -imp fs` + `-fs-mount`/`-fs-data`, mounted via
  `ServeFUSE` with a ctx-cancellable connection, `UnmountFUSE` on shutdown
  (SIGINT/SIGTERM via `signal.NotifyContext`).
- **FUSE adapter** (`src/momofs/fuse.go`): `fuseRoot`/`fuseDir`/`fuseFile`/
  `fuseFileHandle` + `fuseDirHandle`, `toFuseErr` POSIX errno mapping.
- **URL-sized writes**: the write model buffers per-handle and **materializes a
  whole blob on flush/release** — matching momofs' write-whole-file CAS design;
  reads stream via `*FS.ReadAt`.
- **Testing**: node/handle unit tests + a `/dev/fuse`-gated kernel e2e
  (self-skips in CI where `/dev/fuse` absent) + goleak/`-race`.

## Implemented-state note (Rule 76)

The POSIX write model and metadata semantics above are **implemented**
(ratified by `src/momofs` + `docs/momofs/MOUNT_USER_GUIDE.md` + `IMPLEMENTATION.md
§2.3`). What remains *planned* per the spec's follow-ups: full
`mmap`-byte-range correctness and POSIX locks (writes are per-handle-buffered,
whole-blob-on-flush today).

## ⚡ Bolt + Sentinel lens

- **Bolt**: bounded per-handle write buffers; no unbounded memory growth for
  large in-flight writes.
- **Sentinel**: unmount *must* be clean (a stale fuse mount on crash = partial
  tree); scatter changes stay lease-validating so rename/refcount survives.

## Related

Core: [022](022-momofs-posix-core.md). Byte layer: [004](004-cas-content-addressable-store.md).