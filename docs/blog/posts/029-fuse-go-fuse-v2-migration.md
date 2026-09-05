---
title: "FUSE Transport: Migrating to go-fuse/v2"
date: 2026-09-01T18:40:07Z
draft: false
tags: [go, fuse, momofs, bolt]
categories: [momofs]
summary: "The momofs FUSE mount traded its hand-rolled bazil.org/fuse adapter for the go-fuse/v2 high-level fs API, mapped all 22 node callbacks to go-fuse interfaces, and deprecated consistency=cached."
artifacts:
  - {type: issue, id: "980"}
  - {type: pr, id: "984"}
  - {type: spec, path: openspec/changes/add-fuse-implementation}
related:
  - 004-cas-content-addressable-store
  - 028-roadmap-and-research
---
The momofs FUSE mount started as a thin bazil.org/fuse adapter over the CAS
store: directories are content-addressed JSON manifests, files are CAS blobs,
and the kernel's byte-range model is reconciled by buffering handle writes and
materializing one blob per file on flush. The adapter itself was hand-rolled —
fine for a prototype, but it re-implemented a lot of wire protocol that a
battle-tested library already owns.

Issue #980 and the ratified `add-fuse-implementation` OpenSpec change picked a
migration path. This post documents what actually shipped (PR #984).

## Why go-fuse/v2

`hanwen/go-fuse/v2` provides a high-level `fs` package on top of the raw FUSE
wire protocol: inode-based trees, kernel-managed node lifetimes, and syscall
errno returns from node callbacks. It owns the error-prone parts (mount
negotiation, `WaitMount`, lookup refcounting, splice) that the prior custom
adapter had to get right by hand.

## What changed

- **`src/momofs/fuse.go`** — the bazil node model (`fs.Node`, `fs.Handle`,
  `*fuse.Attr`) was replaced with go-fuse/v2 `fs` interfaces. The full prior
  callback set maps to go-fuse equivalents: `Lookup`/`Getattr`/`Setattr`/
  `Mkdir`/`Create`/`Unlink`/`Rmdir`/`Rename`/`Link`/`Opendir`/`Readdir`/`Open`/
  `Read`/`Write`/`Flush`/`Release`/`Statfs`/`OnForget`. `ServeFUSE` and
  `UnmountFUSE` keep their signatures (the CLI and e2e test call them
  unchanged), and `UnmountFUSE` now tracks live servers by mountpoint.
- **Error mapping** — go-fuse wants `syscall.Errno` returns. The adapter
  preserves the errno the momofs core already picks (`ENOENT`/`EISDIR`/
  `EINVAL`/...) and maps anything else to `EIO`; a panic guard (two-line
  recover → `EIO`) keeps a stray panic from tearing down the whole mount.
- **`[momofs]` config section** — new optional section. `consistency=cached`
  is deprecated: kernel-level DAX/VirtioFS consistency makes it redundant, so
  it is ignored with an AUDIT log and any other value is rejected at config
  load.
- **Dependency swap** — `bazil.org/fuse` removed from the module and vendor
  tree; `github.com/hanwen/go-fuse/v2 v2.11.0` added as a direct dependency.

## Read path and splice

The momofs core is a hash-addressed blob reader — reads flow through
`FS.ReadAt` into a memory view, so the natural go-fuse return is
`fuse.ReadResultData` rather than a kernel-splice `ReadResultFd` (there is no
backing fd to splice from). The zero-copy splice path is a documented follow-up
for blob backends that expose raw fds.

## Verification

- `make test` (vet + race + cover) green across all 9 modules.
- `TestFuseE2E_MountRoundTrip` passed against a real `/dev/fuse` mount:
  native writes surfaced in the mount and mount writes surfaced natively.
- `go vet`, `gofmt -l`, and `go work vendor` parity clean; bazil is gone from
  `go.mod` and `vendor/`.

Follow-ups tracked on #980: cross-platform fallback (macOS VirtioFS warning)
and Phase-5 load measurements.

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md), the transport layers follow the ⚡ Bolt
(performance, minimize syscalls/copies) and 🛡 Sentinel (fail-closed, honest
error semantics) mindsets; the go-fuse/v2 migration keeps the momofs core
protocol-agnostic so no momofs wire format is locked to the transport.