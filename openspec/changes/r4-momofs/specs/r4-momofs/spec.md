> GitHub Issue URL: https://github.com/alsotoes/momo/issues/932

# r4-momofs Specification

## Purpose
Ratify the `docs/momofs/` design set as the spec baseline and specify the minimum
architecture for a production POSIX filesystem surface (FUSE mount) over the replicated
content-addressed store.

## Baseline & scope boundary

### R4-C0: Ratify momofs design
- `docs/momofs/{README,ARCHITECTURE,DESIGN_DECISIONS,DESIGN_PRINCIPLES,IMPLEMENTATION,
  PERFORMANCE_SECURITY,RECOVERY,SCRUB_HEALING,MULTI_TENANCY,GDPR,COMPARISON,LIMITATIONS,
  ROADMAP}.md` are ratified as the authoritative design. Conflicts between this spec and
  those docs MUST be resolved in favor of this spec (it is newer); discrepancies noted in
  R4-L1.

## Surface

### R4-C1: FUSE mount
- Provide a `momo fs mount <dir>`/`momofs` entrypoint that mounts the cluster as a POSIX
  tree. Use an existing Go FUSE binding (e.g. `bazil.org/fuse`); pin + vendor per Rule 25.
- Operations: lookup, getattr/setattr, readdir, open/create/read/write, mkdir/rmdir,
  unlink, rename, link, symlink? (ratify per build opts), fsync, getxattr/setxattr,
  posix-locks.

### R4-C2: Correct metadata semantics
- Atomic rename: rename MUST be atomic per the backing store (single namespace op).
- Hardlinks: multiple directory entries may reference the same content hash; refcount kept
  correct (align with CAS GC refcounting).
- Permissions/ownership: mode/uid/gid stored as object metadata and honored on access;
  root-squash or configurable enforcement per DESIGN_DECISIONS.md.
- mmap/read-write export and byte-range semantics correct for consumer apps.

### R4-C3: Single backing store consistency
- Objects written/overwritten via S3 or momo-native MUST appear in the mount, and mount
  writes MUST be visible via S3/native, bounded by documented cache incoherence window
  (entry TTL + on-invalidate refetch).
- Content key = `H(plaintext)` unless envelope-E2EE changes addressing (document interplay
  with `encryption_enabled`/`e2ee_key`).

### R4-C4: Robustness
- Panic-free ops (Rule 37), bounded memory, crash recovery on remount (replay/invalidate
  stale entries), and coexistence with scrub/heal (R2) and GC.

## Tests

### R4-T1
- POSIX smoke: create/write/read/rename/unlink/mkdir/hardlink round-trips over the mount.
### R4-T2
- Consistency: S3 PUT visible via mount; mount write visible via S3/native within TTL.
### R4-T3
- Atomic rename + refcount under hardlinking; GC keeps live entries.
### R4-T4
- Crash/remount: stale entries invalidated; goleak + `-race`.

## Non-Requirements
- Full POSIX ACLs, quotas, and snapshots (out of scope per DESIGN_DECISIONS.md/LIMITATIONS.md);
  documented as future.
