---
title: "FUSE Transport: Migrating to go-fuse/v2"
date: 2026-09-01T18:40:07Z
draft: false
post_type: architecture
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
The first version of our FUSE mount worked. Directories were content-addressed
JSON manifests, files were content-addressed blobs, and a hand-rolled adapter
reconciled the kernel's byte-range writes with our write-whole-file model. It also
re-implemented a surprising amount of FUSE wire protocol — mount negotiation,
lookup reference counting, kernel coordination — that we had no business owning.
The adapter was fine as a prototype and wrong as a foundation. This is the story
of trading it for a library that already knows the protocol.

## The Real Problem

**FUSE** is the kernel bridge that lets a userspace program be a filesystem: the
kernel forwards file operations to your process over `/dev/fuse`. The bridge has a
precise wire protocol with sharp edges. Get mount negotiation subtly wrong and the
mount hangs or dies at a bad moment. Mishandle lookup reference counting and the
kernel forgets an inode your process still believes in, or never forgets one you
released. These are not bugs you find in testing; they are bugs you find in
production, intermittently.

Our adapter had implemented all of this by hand against the raw protocol. It was
correct enough to mount and pass round-trips, but every edge was ours to maintain,
and the effort bought us nothing a mature library could not provide. Meanwhile
the filesystem needed to grow: 22 node callbacks, each one a place to get the
protocol details wrong.

## Why the Obvious Solutions Failed

**Keep hardening the hand-rolled adapter.** This is the sunk-cost answer, and it
is sometimes right. It was wrong here because the protocol work was pure
duplication — the same negotiation and refcounting every FUSE filesystem needs —
so every hour spent on it was an hour not spent on storing bytes correctly.

**Adopt a low-level binding.** There are FUSE libraries that expose the raw
protocol as opaque byte operations: you get the messages, but you still own inodes,
lookups, and error semantics yourself. That would have traded one hand-written
protocol layer for another with fancier types, and left the hard bookkeeping
intact.

**Switch transports entirely (e.g. VirtioFS on every platform).** On macOS,
Docker Desktop already serves files through the kernel's VirtioFS, and no momofs
FUSE process is needed at all. But on bare-metal Linux we still need a userspace
mount, so "use VirtioFS everywhere" is only half a plan. We kept both: VirtioFS
where the platform provides it, and a real FUSE transport on Linux.

## The Solution: Move to go-fuse/v2's High-Level API

`hanwen/go-fuse/v2` is a mature Go FUSE implementation. Its `fs` package provides
a **high-level, inode-based API**: you describe a tree of nodes and implement
methods on them, and the library owns the protocol underneath — mount negotiation,
kernel-managed node lifetimes, and translating returned errors into the codes the
kernel expects. That is exactly the division of labor we wanted.

Issue #980, with the ratified `add-fuse-implementation` spec, chose this path.
Pull request #984 is what shipped.

The migration was mostly a re-expression of what we already had:

- **The node model was replaced.** Our previous node and handle types, and their
  attribute structs, were rewritten as the go-fuse `fs` interfaces. The full prior
  callback set — lookup, getattr, setattr, mkdir, create, unlink, rmdir, rename,
  link, opendir, readdir, open, read, write, flush, release, statfs, forget — now
  maps one-to-one onto go-fuse methods. Crucially, the public entry points the CLI
  and end-to-end test call kept their signatures, so nothing above the adapter
  changed: the mount command and its tests were untouched. Unmount now tracks live
  servers by mount point, so cleanup is per-mount rather than global.
- **Error mapping got explicit.** go-fuse wants methods to return a system error
  code. The adapter preserves the precise code the core already chooses — "not
  found," "is a directory," "invalid argument" — and maps anything unknown to a
  generic I/O error. We also added a panic guard: a two-line recover converts a
  stray panic into an I/O error, so one bad operation cannot tear down the whole
  mount.

```go
// go-fuse wants a syscall.Errno back. Preserve the specific code the core picked
// (so "no such file" stays ENOENT, not a vague failure), and fail closed to EIO
// for anything unexpected. The deferred recover means a panic in a callback
// returns EIO for that one call instead of killing the mount for every process.
func toErrno(err error) syscall.Errno {
    if err == nil {
        return 0
    }
    var errno syscall.Errno
    if errors.As(err, &errno) {
        return errno // ENOENT, EISDIR, EINVAL, ... — as the core intended
    }
    return syscall.EIO
}
```

- **A new `[momofs]` config section.** The old `consistency=cached` setting is
  **deprecated.** It used to let the mount cache aggressively, but kernel-level
  consistency mechanisms (DAX, and VirtioFS on macOS) make that redundant — the
  kernel already guarantees what the flag was emulating. Setting it now logs an
  audit message and is ignored; any other value is rejected when the config loads,
  so a typo cannot silently change caching behavior.
- **The dependency was swapped.** The previous binding was removed from both the
  module and the vendored tree, and go-fuse/v2 was added as a direct dependency.

## The Read Path and splice

There is a zero-copy read path in FUSE called **splice**, where the kernel copies
data directly from a file descriptor into a socket without bouncing through
userspace. It only helps when there is a real file descriptor to splice from. Our
core is a hash-addressed blob reader: reads flow into a memory view, so there is
no backing descriptor to hand the kernel. The natural return is therefore "here is
the data," not "here is the descriptor." The zero-copy splice path is documented
as a follow-up for blob backends that *can* expose raw descriptors — we chose the
honest implementation over a nominal optimization.

## How We Verified

- The full test target (vet, race detector, coverage) passed across all modules.
- The end-to-end mount test ran against a real `/dev/fuse` mount and passed:
  writes made natively surfaced inside the mount, and writes made through the
  mount surfaced natively.
- `go vet` and formatting checks were clean, the vendored tree stayed consistent,
  and the old binding was confirmed gone from the module.

Remaining follow-ups are tracked on issue #980: a cross-platform fallback (the
macOS VirtioFS guidance) and load measurements for the new path.

## Implemented vs. Planned (Honesty Note)

The go-fuse/v2 migration and the `consistency=cached` deprecation are shipped.
Still planned, per the spec's task list: a cross-platform FUSE fallback, splice
support for descriptor-backed blob stores, and the Phase-5 load and memory
measurements. Treat those as open work, not delivered behavior.

## ⚡ Bolt + 🛡 Sentinel Lens

Per [docs/STANDARDS.md](../../STANDARDS.md), the transport follows two mindsets:

- **⚡ Bolt**: go-fuse owns the syscall-heavy protocol work, and the core stays
  protocol-agnostic, so no momofs wire format is locked to the transport — we can
  improve the byte path without renegotiating FUSE.
- **🛡 Sentinel**: failing closed is explicit. Unknown errors become a generic I/O
  error rather than optimism, the panic guard isolates a bad callback, and a
  deprecated config value is rejected instead of silently reinterpreted.

## Failure Modes

| Risk | Guard |
|------|-------|
| A panic in one callback kills the mount | `recover` converts it to an I/O error for that call |
| Unknown error leaks as success | Unmapped errors fail closed to `EIO` |
| `consistency=cached` silently changes behavior | Deprecated: logged, ignored, other values rejected at load |
| Inode never forgotten → leak | go-fuse owns node lifetimes and forget handling |
| Public entry points changed under callers | `Serve`/`Unmount` signatures preserved; CLI e2e untouched |

## When NOT to Use This

- **When you need kernel-level zero-copy today.** Our blob reader has no descriptor
  to splice from, so splice is not a win until a backing store exposes one.
- **Non-Linux hosts.** This transport targets Linux FUSE; macOS uses VirtioFS
  through Docker Desktop and needs no momofs FUSE process.

## References / Dig deeper

- Spec: `openspec/changes/add-fuse-implementation`.
- Issue #980; pull request #984.
- The core it mounts: [004: Content-Addressable Storage](004-cas-content-addressable-store.md).
- Where the migration sits: [028: Roadmap and Research](028-roadmap-and-research.md).
- The transport it replaced: [023: momofs FUSE Transport](023-momofs-fuse-transport.md).
