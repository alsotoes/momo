# Change: R4 — momofs FUSE/POSIX filesystem layer

**Related Issues:**
- https://github.com/alsotoes/momo/issues/932 (R4)
- https://github.com/alsotoes/momo/issues/928 (roadmap parent)

## Why

Momo is a replicated content-addressed object store + S3 gateway, but is **not** a POSIX
filesystem. The extensive `docs/momofs/` design set (IMPLEMENTATION.md, ARCHITECTURE.md,
DESIGN_DECISIONS.md, PERFORMANCE_SECURITY.md, RECOVERY.md, SCRUB_HEALING.md, MULTI_TENANCY.md,
GDPR.md, COMPARISON.md, LIMITATIONS.md) is design-only — there is no `momofs` source, no
mount command, and no FUSE integration. To be a production **distributed filesystem**, momo
needs a mountable POSIX surface with correct metadata semantics over the replicated CAS.

## What

Implement the momofs FUSE/POSIX layer ratifying the existing design docs as the spec
baseline:

1. **Mountable FUSE filesystem** exposing the momo cluster as a POSIX tree
   (directories, files, renames, hardlinks) over the content-addressed object store.
2. **Correct metadata semantics**: atomic rename, hardlinks, mode/owner/permissions,
   correct mmap read/write exports and POSIX locks.
3. **Native S3/momo access consistency**: objects written over S3/momo-native appear in
   the mount and vice-versa (single backing store).
4. **Robustness**: crash recovery, entry caching with invalidation, panic-free operations
   per project hardening rules.

## Out of scope (per DESIGN_DECISIONS.md/IMPLEMENTATION.md)
- See the momofs design docs for explicit out-of-scope items (e.g. full POSIX ACLs, quota,
  snapshots). Ratify those boundaries here.
