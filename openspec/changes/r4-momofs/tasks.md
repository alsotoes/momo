# Tasks: R4 — momofs FUSE/POSIX filesystem layer (#932)

_Design is already authored in `docs/momofs/` (IMPLEMENTATION, ARCHITECTURE, DESIGN_DECISIONS,
etc.). This change migrates it from design to ratified spec and then to implementation.
Items below are implementation phases._

## 1. Ratify design + foundation
- [x] Ratify docs/momofs DESIGN as the spec baseline; design-vs-implementation gaps recorded in PR (R4-C0)
- [ ] Select + pin Go FUSE binding (e.g. `bazil.org/fuse`); `go work sync` + vendor (Rule 25)

## 2. Core mount + metadata (`momofs` package)
- [ ] `momo fs mount` entrypoint + FUSE connection lifecycle
- [x] Inode/metadata layer over CAS (dirs as content-addressed manifests, files as blobs) (R4-C1)
- [x] lookup/getattr/setattr/readdir/open/create/read/write ops (R4-C1)
- [x] Permission/ownership enforcement (mode/uid/gid) — R4-C2

## 3. Metadata semantics + consistency
- [x] Atomic rename + hardlink (refcount-aligned with CAS GC) — R4-C2
- [x] S3/native ↔ mount visibility (single store, fresh manifest reads) — R4-C3
- [ ] mmap/byte-range correctness; posix-locks — R4-C2

## 4. Robustness
- [x] Panic-free op handling (Rule 37), bounded memory
- [x] Crash/remount recovery (fresh manifests on remount) — R4-C4
- [ ] Coexist with scrub/heal (R2) + GC

## 5. Tests (`tests` or package e2e)
- [x] R4-T1 POSIX smoke round-trips
- [x] R4-T2 S3↔mount consistency
- [x] R4-T3 atomic rename + hardlink refcount under GC
- [x] R4-T4 crash/remount; goleak + `-race`

## 6. Validation
- [ ] `make build` + full `make test` green
- [ ] CI workflow for mount e2e (smoke) added
- [ ] Docs: `docs/momofs/` marked implemented; ROADMAP R4 done; user guide for mount
