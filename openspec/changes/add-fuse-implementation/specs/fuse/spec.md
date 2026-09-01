# FUSE Implementation Specification

## Purpose

Define the FUSE implementation to use for momofs going forward, including requirements for each supported path and consistency model guarantees.

## ADDED Requirements

### Requirement: macOS VirtioFS (Resolves fuse-change)
Docker Desktop on macOS uses VirtioFS as the file-sharing implementation. momofs **SHALL NOT** implement its own FURE transport for macOS, as Docker Desktop provides it natively.

- **GIVEN** a macOS host running Docker Desktop
- **WHEN** momofs is configured for blob storage
- **THEN** the host uses Docker's VirtioFS transport, not momofs' FUSE implementation
- **AND** `consistency=cached` flag **SHALL** be ignored or rejected with advisory message (kernel provides native consistency)

### Requirement: Linux go-fuse/v2 Migration (Resolves fuse-change)
For Linux deployments (bare-metal or Docker), momofs **SHALL** migrate from the custom FUSE wire protocol to `hanwen/go-fuse/v2` with the following sub-requirements:

#### go-fuse/v2 Integration
- **GIVEN** a Linux host with go 1.21+
- **WHEN** building momofs with `GOFLAGS="-tags fuse"`
- **THEN** the build links against `go-fuse/v2` providing the FUSE wire protocol
- **AND** all 22 FUSE callbacks in `src/momofs/fuse.go` **SHALL** be replaced with `go-fuse/v2` handler adaptations
- **AND** splice/ReadResultPipe **SHALL** be used for zero-copy data transfer where kernel supports it

#### FUSE Mount Options
- **GIVEN** a Linux FUSE mount of momofs
- **WHEN** the mount options are presented to the user
- **THEN** `consistency=cached` **SHALL NOT** be a valid mount option (kernel provides native consistency)
- **AND** `user_allow_other` **SHALL** be configured if multi-tenant access is required
- **AND** `default_permissions` **SHALL** be configured for unprivileged operation

### Requirement: consistency=cached Flag Deprecation (Resolves fuse-change)
The `consistency=cached` configuration option **SHALL** be deprecated and **SHALL** emit an AUDIT-level log message when set.

- **GIVEN** `consistency = "cached"` in `[momofs]` config section
- **WHEN** momofs starts or reloads configuration
- **THEN** an AUDIT log entry records: `consistency=cached is redundant with kernel-level DAX consistency; ignoring`
- **AND** the flag **SHALL** be ignored (no behavioral effect)
- **AND** future config versions **SHALL** omit this field

### Requirement: Cross-Platform Path (Resolves fuse-change)
For platforms without native VirtioFS (e.g., bare-metal Linux without Docker, Windows):

- **GIVEN** momofs runs on a platform without Docker/VirtioFS
- **WHEN** the user configures `[momofs]` section
- **THEN** the system **SHALL** fall back to `go-fuse/v2` with graceful degradation from splice (zero-copy) to standard read/write path
- **AND** an informative WARNING log entry guides the user toward Docker Desktop on macOS for the best experience

### Requirement: No Extra Daemon Requirement (Resolves fuse-change)
momofs **SHALL** NOT require a separate FUSE daemon process (e.g., fusermount, gRPC-FUSE bridge).

- **GIVEN** momofs is running
- **WHEN** checking running processes
- **THEN** no `fusermount`, `rpc-statd`, or gRPC-FUSE daemon processes **SHALL** be attributed to momofs
- **AND** on platforms where a daemon would be required (Windows without WinFsp), an error **SHALL** be returned at startup guiding the user to alternative implementations

### Requirement: Security Surface Reduction (Resolves fuse-change)
momofs **SHALL** reduce the security surface compared to the current custom FUSE implementation:

- **GIVEN** a default momofs installation
- **WHEN** running under unprivileged user namespace
- **THEN** the process **SHALL** start without capability drops beyond `CAP_SYS_ADMIN` being voluntarily relinquished
- **AND** no FUSE-specific socket activation (e.g., `fsxd`, `fuse2`) **SHALL** be required
- **AND** memory-safe Go types **SHALL** replace any raw pointer arithmetic from the prior custom protocol

---

## CHANGED Requirements (from prior implementation)

###Requirement: Custom FUSE Wire Protocol (REMOVED)
The prior custom FUSE wire protocol embedded in `src/momofs/fuse.go` **SHALL** be removed.

- **GIVEN** the prior 22-function FUSE callback set
- **WHEN** momofs builds with the new go-fuse/v2 integration
- **THEN** `src/momofs/fuse.go` **SHALL** be replaced with `go-fuse/v2` adapter code
- **AND** the 22 callback signatures **SHALL** map as follows:

| Prior Callback | go-fuse/v2 Equivalent |
|---|---|
| `Attr` | `fsys.Attr` |
| `Getattr` | `fsys.Getattr` |
| `Setattr` | `fsys.Setattr` |
| `Open` | `fsys.Open` |
| `Read` | `fsys.Read` (with ReadResultPipe support) |
| `Write` | `fsys.Write` |
| `Flush` | `fsys.Flush` |
| `Release` | `fsys.Release` |
| `Forget` | `fsys.Forget` |
| `Init` | `fsys.Init` |
| `Writepages` | `fsys.Writepages` (splice) |
| `Destroy` | `fsys.Destroy` |
| `Rename` | `fsys.Rename` |
| `Getlk` | `fsys.Getlk` |
| `Setlk` | `fsys.Setlk` |
| `Bmap` | `fsys.Bmap` |
| `Destroy` | `fsys.Destroy` |
| `FlagChmod` | `fsys.FlagChmod` |
| `FlagChown` | `fsys.FlagChown` |
| `FillSuperblock` | `fsys.FillSuperblock` |
| `Destroy` | `fsys.Destroy` |

---

## Validation

- [ ] Build momofs with `go-fuse/v2` on Linux — all 22 callbacks adapted, no compile errors
- [ ] Verify zero-copy splice path functional with `fallocate(FALLOC_FL_COLLAPSE_RANGE)` + read performance >= 800 MB/s
- [ ] Verify `consistency=cached` is rejected with AUDIT log and no behavioral effect
- [ ] Verify no FUSE daemon processes appear in `ps aux`
- [ ] Run integration test: mount momofs on Linux, perform 1000 upload/download cycles, verify data integrity
- [ ] On macOS: verify Docker VirtioFS is used (no momofs FUSE process); `consistency=cached` logged as AUDIT and ignored

---

## Changed Files

- `src/momofs/fuse.go` — replaced with `go-fuse/v2` adapter (22 callbacks mapped)
- `src/momofs/config.go` — `consistency` field deprecated, emits AUDIT if set
- `src/momofs/daemon.go` — injection of `go-fuse/v2` Filesystem interface
- `openspec/changes/add-fuse-implementation/` — this specification

---

Related: [#820 P2 S3 integrity checksums](https://github.com/alsotoes/momo/issues/820), [#903 core-integrity verification](https://github.com/alsotoes/momo/issues/903)