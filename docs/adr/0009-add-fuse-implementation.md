# 0009-add-fuse-implementation

## Status
Proposed

## Confidence
Low

## Context


## Decision
- macOS VirtioFS (Resolves fuse-change): Docker Desktop on macOS uses VirtioFS as the file-sharing implementation. momofs **SHALL NOT** implement its own FURE transport for macOS, as Docker Desktop provides it natively. - **GIVEN** a macOS host running Docker Desktop - **WHEN** momofs is configured for blob storage - **THEN** the host uses Docker's VirtioFS transport, not momofs' FUSE implementation - **AND** `consistency=cached` flag **SHALL** be ignored or rejected with advisory message (kernel provides native consistency)
- Linux go-fuse/v2 Migration (Resolves fuse-change): For Linux deployments (bare-metal or Docker), momofs **SHALL** migrate from the custom FUSE wire protocol to `hanwen/go-fuse/v2` with the following sub-requirements: - **GIVEN** a Linux host with go 1.21+ - **WHEN** building momofs with `GOFLAGS="-tags fuse"` - **THEN** the build links against `go-fuse/v2` providing the FUSE wire protocol - **AND** all 22 FUSE callbacks in `src/momofs/fuse.go` **SHALL** be replaced with `go-fuse/v2` handler adaptations - **AND** splice/ReadResultPipe **SHALL** b...
- consistency=cached Flag Deprecation (Resolves fuse-change): The `consistency=cached` configuration option **SHALL** be deprecated and **SHALL** emit an AUDIT-level log message when set. - **GIVEN** `consistency = "cached"` in `[momofs]` config section - **WHEN** momofs starts or reloads configuration - **THEN** an AUDIT log entry records: `consistency=cached is redundant with kernel-level DAX consistency; ignoring` - **AND** the flag **SHALL** be ignored (no behavioral effect) - **AND** future config versions **SHALL** omit this field
- Cross-Platform Path (Resolves fuse-change): For platforms without native VirtioFS (e.g., bare-metal Linux without Docker, Windows): - **GIVEN** momofs runs on a platform without Docker/VirtioFS - **WHEN** the user configures `[momofs]` section - **THEN** the system **SHALL** fall back to `go-fuse/v2` with graceful degradation from splice (zero-copy) to standard read/write path - **AND** an informative WARNING log entry guides the user toward Docker Desktop on macOS for the best experience
- No Extra Daemon Requirement (Resolves fuse-change): momofs **SHALL** NOT require a separate FUSE daemon process (e.g., fusermount, gRPC-FUSE bridge). - **GIVEN** momofs is running - **WHEN** checking running processes - **THEN** no `fusermount`, `rpc-statd`, or gRPC-FUSE daemon processes **SHALL** be attributed to momofs - **AND** on platforms where a daemon would be required (Windows without WinFsp), an error **SHALL** be returned at startup guiding the user to alternative implementations
- Security Surface Reduction (Resolves fuse-change): momofs **SHALL** reduce the security surface compared to the current custom FUSE implementation: - **GIVEN** a default momofs installation - **WHEN** running under unprivileged user namespace - **THEN** the process **SHALL** start without capability drops beyond `CAP_SYS_ADMIN` being voluntarily relinquished - **AND** no FUSE-specific socket activation (e.g., `fsxd`, `fuse2`) **SHALL** be required - **AND** memory-safe Go types **SHALL** replace any raw pointer arithmetic from the prior custom p...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Partial
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-fuse-implementation/
- Blog: docs/blog/posts/...md
