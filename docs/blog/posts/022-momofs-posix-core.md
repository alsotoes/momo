---
title: "R4: momofs — POSIX Core Over the CAS Store"
date: 2026-08-27T22:40:31Z
draft: false
post_type: architecture
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
For three releases momo was an object store: you wrote bytes under a key and read
them back by that key. Then we asked a question that turned out to be the biggest
architecture bet yet — *can it also behave like a filesystem?* Not a filesystem
bolted on top, with its own separate storage and its own separate consistency
rules, but a filesystem whose substrate is the object store we already had. R4 is
the answer: a POSIX core, tracked by the R4 issue and shipped in the R4 pull
request.

## The Real Problem

An object store and a filesystem disagree about what an object *is*. A filesystem
organizes bytes into a tree of named files and directories with metadata —
permissions, ownership, timestamps — and expects operations like rename to be
atomic. An object store knows only immutable blobs addressed by a key. If we
tried to run both by keeping two separate stores, we'd immediately own two sources
of truth: a file written one way would have to be copied into the other, and the
copies would eventually disagree.

So the real requirement was harsher than "add a filesystem." It was: **one store,
visible through two interfaces.** An object written through the native API must
appear in the filesystem tree, and a file written through the mount must be a real
object — with no syncing, no second database, and no window where the two views
disagree.

We had one asset that made this plausible. The object store is *content-addressed*
(see the CAS post): every blob is named by the SHA-256 hash of its own bytes, so
identical content collides on purpose and deduplicates for free. If the filesystem
layer could be built entirely out of content-addressed objects, consistency would
not be a synchronization problem at all — both interfaces would simply be reading
and writing the same named bytes.

## Why the Obvious Solutions Failed

**A separate metadata database for the tree.** The conventional filesystem design
keeps an inode table and a directory index in their own database. We rejected it
because it recreates exactly the two-sources-of-truth problem we were trying to
avoid: the tree and the object store could drift, and reconciling them would be
someone's full-time job.

**Reusing the existing key/value namespace as the tree.** We could have mapped
`/a/b.txt` onto a single object key `a/b.txt`. That gives you a flat map, not a
filesystem: there is no directory object to `readdir`, no place to store POSIX
metadata per entry, and rename becomes a multi-key rewrite that cannot be atomic.

**Treating directories as empty marker objects.** A zero-byte "directory" object
has nowhere to record its children, so listing a directory means scanning every
key for a prefix — correct but O(n) per `readdir`, and it still cannot express
hardlinks, where two names must point at one underlying file.

Each of these solved the visible problem (expose files) while quietly deferring
the hard part (one consistent substrate). We wanted the opposite trade.

## The Solution: The Tree Is Made of Objects

The R4 core reuses the content-addressed store as the byte layer and invents a
**metadata layer** on top of it. The whole design rests on one mapping:

- **Directories are content-addressed manifests.** A directory is itself a stored
  object whose contents are a list of entries — each entry pairs a name with the
  address of a child (its manifest or blob) and carries POSIX metadata such as
  mode, owner, and group. Because a manifest is an object, it is immutable and
  versioned by its content hash: changing a directory means producing a new
  manifest, and the old one remains addressable.
- **Files are the existing blobs themselves.** File content is hash-addressed
  exactly like any other object, so deduplication applies to file data with no
  extra machinery.
- **Inode metadata rides alongside each manifest or blob.** POSIX attributes are
  not a separate table; they travel with the object that represents the entry.

The payoff is the property we were after. A directory listing such as
`mkdir x` followed by writing a file inside it is, underneath, one manifest object
and one blob object: **a single store that is simultaneously an S3-style key space
and a POSIX tree.** Consistency across the two interfaces falls out for free,
because there is only ever one manifest to read.

{{< diagram src="/diagrams/09-fuse-mount-stack.svg" alt="momofs stack: POSIX calls over CAS objects" caption="Figure 1: The POSIX core maps the kernel's file operations onto content-addressed manifest and blob objects" >}}

```go
// A directory is an object whose payload is its entry list. Each entry names a
// child object by content hash and carries that child's POSIX attributes — so a
// single manifest read answers both "what is here?" and "what are its modes?".
type ManifestEntry struct {
    Name  string
    Child BlobID        // address of the child's manifest (dir) or blob (file)
    Mode  os.FileMode
    UID   int
    GID   int
    Nlink int           // hardlink count, kept aligned with the GC refcount sweep
}
```

## What Came With It

The core implements the operations a filesystem must answer — `lookup`,
`getattr`, `setattr`, `readdir`, `open`, `create`, `read`, and `write` — against
the content-addressed layer rather than a local inode table.

The subtler work was **atomic rename and hardlinks**. A rename must be atomic from
a reader's perspective: either the old name or the new name, never a half-moved
entry. Because a directory is a manifest and manifests are immutable, a rename
rewrites one manifest and swaps it in a single step — a reader sees the old
manifest or the new one, never both. Hardlinks are the reverse pressure: two names
point at one content, so the filesystem has to keep a *reference count* telling
the garbage collector how many live names still need the bytes. We aligned that
count with the tombstone-driven GC sweep, so a hardlinked blob is not reaped while
a name still points at it. That work connected the filesystem directly to the at-rest
integrity and GC arc.

## Implemented vs. Planned (Honesty Note)

The momofs documentation suite is largely a *design and plan* corpus. In this
post the factual, shipped layer is the POSIX core implemented in the R4 pull
request, plus the operational docs we keep in sync with it. Design features that
are *not yet implemented* — full POSIX ACLs, quotas, and snapshots — remain
planned, not shipped, and should not be read as available.

## ⚡ Bolt + 🛡 Sentinel Lens

Per [docs/STANDARDS.md](../../STANDARDS.md):

- **⚡ Bolt**: manifests are small, hot, and read constantly, so metadata handling
  is written to be dense and allocation-light; the byte path rides the existing
  zero-allocation hashing of the CAS layer.
- **🛡 Sentinel**: reference counts are aligned with the garbage-collection sweep,
  so a hardlink cannot permit the collector to reap a still-referenced blob. The
  delete path is a single auditable owner rather than scattered logic.

## Failure Modes

| Risk | Guard |
|------|-------|
| Rename observed half-applied | Manifest swap is a single immutable object replacement |
| Blob reaped while a hardlink still points at it | Refcount tracks live names and gates the tombstone sweep |
| Tree and object view disagree | Both views read the same manifests — there is no second database |
| Planned feature assumed shipped | This post and the design suite label them explicitly |

## When NOT to Use This

- **Mutable, in-place files.** Content addressing rewrites a whole new blob on
  change; this design suits files that are rewritten wholesale, not edited in
  place with fine-grained range updates (a follow-up area).
- **When you only need an object API.** If no POSIX consumer needs the tree, the
  metadata manifests are pure overhead — use the native key space directly.

## References / Dig deeper

- Spec: `openspec/changes/r4-momofs`.
- Pull request #957 (POSIX core); tracking issues #932 (R4) and #962 (FUSE).
- Byte layer: [004: Content-Addressable Storage](004-cas-content-addressable-store.md).
- Placement: [005: CRUSH-lite](005-crush-placement.md).
- Mount it with FUSE: [023: momofs FUSE Transport](023-momofs-fuse-transport.md).
- GC and at-rest integrity: [007: At-Rest Integrity](007-at-rest-integrity-and-gc.md).
- Operational docs: `docs/momofs/MOUNT_USER_GUIDE.md`,
  `docs/momofs/IMPLEMENTATION.md`.
