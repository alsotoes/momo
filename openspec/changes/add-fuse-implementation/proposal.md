# FUSE Implementation Choice for momofs

## Problem Statement

momofs currently implements a FUSE-like interface via 22 callbacks in `src/momofs/fuse.go`. The choice of FUSE implementation affects:
- Performance (throughput, latency, zero-copy capabilities)
- Security (userspace attack surface, kernel integration)
- Platform support (Linux, macOS, cross-platform)
- Operational overhead (daemon management, mount privileges)

## Context

- **momofs** is a FUSE-like filesystem shared between host and container/VM workloads
- Currently implements custom FUSE wire protocol in userspace
- Needs a decision on which underlying FUSE implementation to use going forward

## Stakeholders

- Cluster operators running momofs for blob storage
- Docker Desktop users on macOS (VirtioFS availability)
- Linux container orchestration users
- Security auditors assessing userspace kernel interaction surface

## High-Level Decision

| Criterion | VirtioFS | go-fuse/v2 (Linux FUSE) | cgoFUSE (winfsp/macFUSE) |
|---|---|---|---|
| **Performance** | ⭐⭐⭐⭐⭐ (native DAX shared-memory) | ⭐⭐⭐⭐ (splice, zero-copy) | ⭐⭐ (context-switched IPC) |
| **Security** | ⭐⭐⭐⭐⭐ (kernel-mediated) | ⭐⭐⭐ (unprivileged userspace) | ⭐⭐ (daemon privilege) |
| **Platform** | macOS, Linux (Kernel 4.+) | Linux only | Linux, macOS (macFUSE), Windows (WinFsp) |
| **Daemon required** | No (kernel VFS) | Yes (fusermount) | Yes (cgo fusion) |
| **consistency=cached impact** | Redundant (kernel native) | Required for cache control | Required for cache control |

## Decision

1. **macOS/Docker Desktop**: Leverage Docker Desktop's native VirtioFS implementation — no momofs code changes required. momofs continues to operate; Docker handles the transport.

2. **Linux FUSE compatibility**: Migrate to `hanwen/go-fuse/v2` with splice/ReadResultPipe support, replacing the custom FUSE wire protocol in `src/momofs/fuse.go`. This provides:
   - Pure Go implementation (no cgo overhead)
   - Modern kernel optimizations (splice zero-copy)
   - Unprivileged operation via user namespaces
   - Better maintenance ergonomics (active upstream)

3. **Cross-platform**: Maintain separate paths:
   - Linux: `go-fuse/v2`
   - macOS: Docker VirtioFS (no code change)
   - Windows: Not currently supported (would require WinFsp/cgofuse)

## Open Questions

- Should momofs drop the FUSE abstraction entirely and rely on Docker/VirtioFS for all platforms?
- Is there a requirement for bare-metal Linux FUSE deployment (without Docker)?
- What is the timeline for phasing out the custom FUSE wire protocol?

---

Related: [#820 P2 S3 integrity checksums](https://github.com/alsotoes/momo/issues/820), [#903 core-integrity verification](https://github.com/alsotoes/momo/issues/903)