# Tasks: R4 — momofs FUSE/POSIX filesystem layer (#932)

_Design is already authored in `docs/momofs/` (IMPLEMENTATION, ARCHITECTURE, DESIGN_DECISIONS,
etc.). This change migrates it from design to ratified spec and then to implementation.
Items below are implementation phases._

## 1. Ratify design + foundation
- [ ] Ratify `docs/momofs/*` as spec baseline (R4-C0); record spec-vs-design discrepancies
- [ ] Select + pin Go FUSE binding (e.g. `bazil.org/fuse`); `go work sync` + vendor (Rule 25)

## 2. Core mount + metadata (`momofs` package)
- [ ] `momo fs mount` entrypoint + FUSE connection lifecycle
- [ ] Inode/metadata layer over CAS (dirs as content-addressed entries, files as blobs)
- [ ] lookup/getattr/setattr/readdir/open/create/read/write ops (R4-C1)
- [ ] Permission/ownership enforcement (mode/uid/gid) — R4-C2

## 3. Metadata semantics + consistency
- [ ] Atomic rename + hardlink (refcount-aligned with CAS GC) — R4-C2
- [ ] S3/native ↔ mount visibility (single store, entry TTL + invalidation) — R4-C3
- [ ] mmap/byte-range correctness; posix-locks — R4-C2

## 4. Robustness
- [ ] Panic-free op handling (Rule 37), bounded memory
- [ ] Crash/remount recovery (invalidate stale entries) — R4-C4
- [ ] Coexist with scrub/heal (R2) + GC

## 5. Tests (`tests` or package e2e)
- [ ] R4-T1 POSIX smoke round-trips
- [ ] R4-T2 S3↔mount consistency within TTL
- [ ] R4-T3 atomic rename + hardlink refcount under GC
- [ ] R4-T4 crash/remount; goleak + `-race`

## 6. Validation
- [ ] `make build` + full `make test` green
- [ ] CI workflow for mount e2e (smoke) added
- [ ] Docs: `docs/momofs/` marked implemented; ROADMAP R4 done; user guide for mount
