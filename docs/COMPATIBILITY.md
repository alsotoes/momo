# Momo Platform Compatibility

This document outlines the supported operating systems for building and running the Momo application. It also provides information on dependencies and platform-specific considerations.

## Platform Tiers

Momo's support for various operating systems is categorized into the following tiers:

### Tier 1: Officially Supported

These are the platforms where Momo is actively developed, tested, and expected to perform reliably.

-   **Linux:** (Kernel 4.x and newer) - This is the primary development and production environment for Momo.
-   **FreeBSD:** (12.x and newer) - Fully supported and regularly tested.

### Tier 2: Best-Effort Support

These platforms are expected to work, and may even have specific optimizations, but are not part of the regular, continuous testing cycle.

-   **macOS (Apple Silicon):** The inclusion of specific libraries for the M1 CPU architecture (`go-m1cpu`) indicates that Momo is aware of and should perform well on modern Apple hardware.
-   **DragonflyBSD:** The codebase contains specific system call definitions for DragonflyBSD, so it is expected to compile and run correctly. However, it is not a primary test platform.

### Tier 3: Experimental / Limited Support

These platforms are not officially supported. While Momo might compile or run, functionality is likely to be limited or unstable.

-   **Windows:** Momo's core design relies heavily on POSIX system calls and a Unix-like environment. While some of its dependencies have Windows compatibility (e.g., `go-ole`), the main application is **not expected to run natively on Windows**. Users seeking to run Momo on a Windows machine should use the **Windows Subsystem for Linux (WSL) 2**.
-   **Other Unix-like Systems:** Other POSIX-compliant systems (e.g., OpenBSD, NetBSD) may be able to build and run Momo, but they have not been tested.

## Build Dependencies

-   **Go Compiler:** Go **1.25.10** (pinned in `go.mod`) is required to support modern `quic-go` and atomic memory operations.
-   **Standard C Compiler:** A C compiler like GCC or Clang is needed for certain dependencies that use cgo.
-   **Protocols:** Momo natively supports **TCP**, **QUIC (UDP/TLS 1.3)**, and **S3-compatible** REST gateways. The P2P gossip protocol uses a separate TCP port (default `4450`, configurable via `gossip_port` in `[p2p]` section) and supports optional TLS encryption via the `[p2p] tls_*` options.
-   **Prometheus:** Built-in `/metrics` and `/health` HTTP endpoints (no external agent required). Configured via `prometheus_port` in `[metrics]` section (required section).

## Protocol Feature Parity Matrix

Momo has two distinct API surfaces: the **native** transports (`momo-tcp`, `momo-quic`) speaking the custom binary momo framing, and the **S3 gateway** transports (`s3-tcp`, `s3-quic`) speaking standard HTTP/S3 REST. Every capability that applies to a surface is implemented identically on that surface's transports; the two surfaces differ only where the wire protocol itself is different by design.

Legend: ✅ supported · ❌ not applicable to that API surface (by design)

| Feature | momo-tcp | momo-quic | s3-tcp | s3-quic |
|---|---|---|---|---|
| **Binary momo handshake** (84B plaintext / 20B challenge-response) | ✅ | ✅ | ❌ | ❌ |
| **Handshake auth mechanism** | binary token / challenge-response (HMAC-SHA256) | binary token / challenge-response | SigV4 (`Authorization`/presigned) + Bearer | SigV4 (`Authorization`/presigned) + Bearer |
| **Metadata / payload framing** (192B metadata, ACK) | ✅ | ✅ | ❌ (HTTP framing instead) | ❌ |
| **Replication modes** (None/Chain/Splay/Primary-Splay) | ✅ | ✅ | ✅ (server-side downgrade for external clients) | ✅ |
| **E2EE Phase 3** (AES-GCM-256, tenant keys) | ✅ | ✅ | ✅ | ✅ |
| **Envelope E2EE Phase 4** (client-held `e2ee_key`) | ✅ | ✅ | ✅ (via `s3enc`/`s3dec`) | ✅ |
| **Threshold-OPRF confidential dedup** | ✅ (`'O'` binary mode) | ✅ (`'O'` binary mode) | ✅ (`POST /?momo-oprf-eval`) | ✅ (`POST /?momo-oprf-eval`) |
| **At-rest encryption** (`EncryptedBlobStore` SSE) | ✅ | ✅ | ✅ | ✅ |
| **Outbound S3-blob-store TLS** (issue #774) | ✅ | ✅ | ✅ | ✅ |
| **Streaming AEAD wire format** (chunked + footer) | ✅ | ✅ | ✅ | ✅ |
| **Native list / delete / get** (`'L'`/`'D'`/`'G'`) | ✅ | ✅ | ❌ (S3 REST instead) | ❌ |
| **Rolling idle timeout** (30s `IdleTimeoutConn`) | ✅ | ✅ | ✅ (`s3ReadHeaderTimeout` 10s header + per-op deadlines) | ✅ |
| **TLS** | TLS 1.2+ (optional) | TLS 1.3 (mandatory) | TLS required (or explicit `tls_insecure=true`) | TLS 1.3 (mandatory) |
| **Metrics** (`MetricsHook` / Prometheus) | ✅ | ✅ | ✅ | ✅ |
| **P2P gossip / SWIM / lease / scatter-gather** | ✅ (separate `gossip_port`) | ✅ | ✅ | ✅ |
| **Presigned URLs** (query-string SigV4) | ❌ | ❌ | ✅ | ✅ |
| **aws-chunked streaming upload** (signed/unsigned) | ❌ | ❌ | ✅ | ✅ |
| **Multipart upload** (6 endpoints) | ❌ | ❌ | ✅ | ✅ |
| **CopyObject / batch DeleteObjects** | ❌ | ❌ | ✅ | ✅ |
| **SSE header validation** (AES256 honored; SSE-C/KMS rejected) | ❌ | ❌ | ✅ | ✅ |

### Notes

- **`s3-tcp` vs `s3-quic`** are served by the same `S3Communicator` and are functionally identical; the only difference is the carrier (TCP+TLS vs QUIC/UDP+TLS 1.3).
- **`momo-tcp` vs `momo-quic`** share byte-identical binary framing; the only differences are the carrier, the mandatory-vs-optional TLS posture, and the idempotent `Close` guard (QUIC).
- **Rolling idle timeout:** the native transports use a rolling 30s `IdleTimeoutConn`; the S3 gateway relies on its own bounded header/body deadlines (`s3ReadHeaderTimeout`, per-op and size-proportional deadlines, issue #592/#620) instead of a rolling wrapper, so it is not double-wrapped.
- **P2P gossip** runs on a dedicated `gossip_port` (default `4450`) independent of the four data-plane protocols, so it behaves identically regardless of which transport is configured.

## Known Issues and Considerations

-   **Filesystem Performance:** The performance of file I/O can vary significantly between different operating systems and underlying filesystems.
-   **Networking Stack:** The behavior of the networking stack, particularly regarding TCP congestion control and error handling, can differ between kernels. The polymorphic system in Momo is designed to adapt to these variations, but extreme network conditions may expose platform-specific behavior.
-   **Windows (WSL):** When running under WSL 2, network performance and file I/O may not match bare-metal Linux performance. It is recommended to store the data directories within the Linux filesystem (`/`) rather than accessing them across the Windows mount point (`/mnt/c`).
