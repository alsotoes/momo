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

Momo has two distinct API surfaces: the **native** transports (`momo-tcp`, `momo-quic`) speaking the custom binary momo framing, and the **S3 gateway** transports (`s3-tcp`, `s3-quic`) speaking standard HTTP/S3 REST. Every capability that applies to a surface is implemented identically on that surface's transports; the two surfaces differ only where the wire protocol itself is different by design. Client-driven momo-native RPCs (e.g. threshold-OPRF synonym dedup) are designed to be exercised by momo clients over the native transports, not by third-party S3 clients.

Legend: ✅ supported · ◐ supported as an RPC mirror on a surface it is not natively designed for · ❌ not applicable to that API surface (by design). The **S3 API standard** column compares Momo's `s3-*` implementation against the standard S3 REST protocol (as published by SNIA SM3/The AI Data Storage API, § per feature): **N/A** = the feature is not part of the S3 API; **✅ conforms** = standard-compliant; **⚠ partial/extension** = deviates from or extends the standard.

| Feature | momo-tcp | momo-quic | s3-tcp | s3-quic | S3 API standard |
|---|---|---|---|---|---|
| **Binary momo handshake** (84B plaintext / 20B challenge-response) | ✅ | ✅ | ❌ | ❌ | **N/A** — S3 is HTTPS REST, no binary framing |
| **Handshake auth mechanism** | binary token / challenge-response (HMAC-SHA256) | binary token / challenge-response | SigV4 (`Authorization`/presigned) + Bearer | SigV4 (`Authorization`/presigned) + Bearer | **✅ conforms** — SigV4 + presigned query-string (Bearer is a momo extension) |
| **Metadata / payload framing** (192B metadata, ACK) | ✅ | ✅ | ❌ (HTTP framing instead) | ❌ | **✅ conforms** — HTTP headers + XML payloads |
| **Replication modes** (None/Chain/Splay/Primary-Splay) | ✅ | ✅ | ✅ (server-side downgrade for external clients) | ✅ | **N/A** — server-side momo feature, not part of S3 API |
| **E2EE Phase 3** (AES-GCM-256, tenant keys) | ✅ | ✅ | ✅ | ✅ | **N/A** — momo client encryption, S3 does not define it |
| **Envelope E2EE Phase 4** (client-held `e2ee_key`) | ✅ | ✅ | ✅ (via `s3enc`/`s3dec`) | ✅ | **⚠ extension** — momo derives keys client-side (`s3enc`/`s3dec`); not SSE-C/KMS |
| **Threshold-OPRF confidential dedup** | ✅ (`'O'` binary mode, designed) | ✅ (`'O'` binary mode, designed) | ◐ (`POST /?momo-oprf-eval`, mirror, not designed surface) | ◐ (`POST /?momo-oprf-eval`, mirror, not designed surface) | **N/A** — not part of S3 API |
| **At-rest encryption** (`EncryptedBlobStore` SSE) | ✅ | ✅ | ✅ | ✅ | **⚠ partial** — SSE-S3 AES256 style; SSE-C/SSE-KMS not supported (see SSE row) |
| **Outbound S3-blob-store TLS** (issue #774) | ✅ | ✅ | ✅ | ✅ | **✅ conforms** — TLS 1.2+ transport (issue #774) |
| **Streaming AEAD wire format** (chunked + footer) | ✅ | ✅ | ✅ | ✅ | **N/A** — momo's own format; S3 streaming = `aws-chunked` (row below) |
| **Native list / delete / get** (`'L'`/`'D'`/`'G'`) | ✅ | ✅ | ❌ (S3 REST instead) | ❌ | **✅ conforms** — Object-level ops map to `ListObjectsV2` / `GetObject` / `DeleteObject` |
| **Bucket operations** | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `ListBuckets`, `GetBucketLocation`, `HeadBucket`, `CreateBucket`, `DeleteBucket` (empty only) |
| **ListObjectsV2 pagination & prefixes** | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `prefix`, `delimiter` (+ `CommonPrefixes`), `max-keys` (≤1000), `continuation-token`, `start-after`, `fetch-owner` |
| **Object HTTP semantics** (Range, conditional GET) | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `Range`→`206 Partial Content`, `If-Match`(412), `If-None-Match`(304), `If-Modified-Since`, `If-Range`, ETag, Last-Modified |
| **Preserved object metadata** (Content-Type, `x-amz-meta-*`, cache headers) | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — stored at rest and echoed on GET/HEAD (issue #772) |
| **Rolling idle timeout** (30s `IdleTimeoutConn`) | ✅ | ✅ | ✅ (`s3ReadHeaderTimeout` 10s header + per-op deadlines) | ✅ | **N/A** — not part of S3 API |
| **TLS** | TLS 1.2+ (optional) | TLS 1.3 (mandatory) | TLS required (or explicit `tls_insecure=true`) | TLS 1.3 (mandatory) | **✅ conforms** — HTTPS/TLS; `tls_insecure=true` is a non-standard opt-out |
| **Metrics** (`MetricsHook` / Prometheus) | ✅ | ✅ | ✅ | ✅ | **N/A** — not part of S3 API |
| **P2P gossip / SWIM / lease / scatter-gather** | ✅ (separate `gossip_port`) | ✅ | ✅ | ✅ | **N/A** — not part of S3 API |
| **Presigned URLs** (query-string SigV4) | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `X-Amz-Credential`/`X-Amz-Signature` query-string auth |
| **aws-chunked streaming upload** (signed/unsigned) | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `Content-Encoding: aws-chunked`, `STREAMING-AWS4-HMAC-SHA256-PAYLOAD` / `UNSIGNED-PAYLOAD` |
| **Multipart upload** (6 endpoints) | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `CreateMultipartUpload` / `UploadPart` / `ListParts` / `ListMultipartUploads` / `CompleteMultipartUpload` / `AbortMultipartUpload` (issue #764) |
| **CopyObject / batch DeleteObjects** | ❌ | ❌ | ✅ | ✅ | **✅ conforms** — `PUT ...&x-amz-copy-source` (CopyObject), `POST /?delete` (batch delete) |
| **SSE header validation** (AES256 honored; SSE-C/KMS rejected) | ❌ | ❌ | ✅ | ✅ | **⚠ partial — intentional** — honors `AES256` (SSE-S3); rejects `aws:kms` (SSE-KMS) and SSE-C with clear errors stating momo's real guarantee: all objects encrypted at rest with momo AES-256-GCM when `encryption_enabled`, no AWS KMS, customer keys never stored (issues #776, #820 P1) |
| **Integrity checksums** (`x-amz-checksum-*`) | ❌ | ❌ | ✅ | ✅ | **⚠ partial** — verifies CRC32/CRC32C/SHA1/SHA256 on upload (single-part, aws-chunked, multipart), persists + echoes them, and computes on GET with `x-amz-checksum-mode: ENABLED` (issue #820, Tier P2); aws-chunked trailing-checksum form (`x-amz-trailer`) not implemented |
| **Unsupported subresource → `501 NotImplemented`** | ❌ | ❌ | ✅ | ✅ | **⚠ partial — intentional** — known-but-unsupported subresource query params (bucket-config set, issue #912/#913/#920; object-level set, issue #914/#915/#920, incl. `?select`) and `UploadPartCopy` (`PUT ?uploadId&partNumber` + `X-Amz-Copy-Source`) return a clean `501` instead of silently falling through to the nearest method handler |

### Notes

- **`s3-tcp` vs `s3-quic`** are served by the same `S3Communicator` and are functionally identical; the only difference is the carrier (TCP+TLS vs QUIC/UDP+TLS 1.3).
- **`momo-tcp` vs `momo-quic`** share byte-identical binary framing; the only differences are the carrier, the mandatory-vs-optional TLS posture, and the idempotent `Close` guard (QUIC).
- **Rolling idle timeout:** the native transports use a rolling 30s `IdleTimeoutConn`; the S3 gateway relies on its own bounded header/body deadlines (`s3ReadHeaderTimeout`, per-op and size-proportional deadlines, issue #592/#620, issue #816) instead of a rolling wrapper, so it is not double-wrapped.
- **Reading the `S3 API standard` column:** it grades only the `s3-tcp` / `s3-quic` cells against the standard S3 protocol. Rows marked `N/A` are momo-internal capabilities the standard does not govern; rows below `⚠` are the main known gaps/extensions (SSE-C/SSE-KMS via the SSE validation row, and the custom `s3enc`/`s3dec` envelope path).
- **Security / SSE boundary (honest posture, issue #820 P1):** with `encryption_enabled`, **every** S3 object is encrypted at rest with momo AES-256-GCM (`EncryptedBlobStore`, independent of S3 SSE headers). The gateway honors `x-amz-server-side-encryption: AES256` (accurate; SSE-S3-equivalent). It **rejects** SSE-C (customer keys — never stored) with `400 InvalidRequest` and SSE-KMS (`aws:kms` — no AWS KMS integration) with `501 NotImplemented`, each error stating momo's real guarantee. Faking SSE-C/KMS is intentionally avoided (misrepresentation breaks audit/compliance). See `ARCHITECTURE.md` §4c. Workloads that require SSE-C/SSE-KMS must enforce it upstream.
- **Threshold-OPRF on S3 (`◐`):** OPRF is a momo-native client handshake; a momo client uses `momo-tcp`/`momo-quic` for OPRF evaluation. The `POST /?momo-oprf-eval` endpoint on the S3 gateway is an RPC mirror of the native evaluation, provided for completeness and driven by `S3Communicator.SendOPRFEval`. It is **not a designed parity surface** — standard S3 clients cannot perform OPRF, so it has no designed consumer (issue #817, closed as not-planned).
- **P2P gossip** runs on a dedicated `gossip_port` (default `4450`) independent of the four data-plane protocols, so it behaves identically regardless of which transport is configured.
- **Standard S3 features not implemented:** those NOT listed above are absent by scope. Aware scope gaps include versioning, bucket policies / ACLs, bucket `?cors`/`?website`/`?lifecycle`/`?tagging`, object lock / retention, `UploadPartCopy`, bucket-level SSE/config (`?encryption`), the aws-chunked trailing-checksum form (`x-amz-trailer`), and SelectObjectContent. Query-subresource gaps are **not** silently fall-through: the gateway intercepts a known unsupported subresource set at the dispatch root and returns a clean `501 NotImplemented` (bounded write, store-independent), for both bucket-config (`?versioning`/`?versions`, `?acl`, `?policy`, `?cors`, `?website`, `?lifecycle`, `?tagging`, `?encryption`, `?publicAccessBlock`, `?accelerate`, `?replication`, `?requestPayment`, `?logging`, `?object-lock`, `?notification`, `?analytics`, `?inventory`, `?metrics`, `?intelligent-tiering`) and object-level (`?tagging`, `?acl`, `?versionId`, `?retention`, `?legal-hold`, `?select`) subresources; `UploadPartCopy` (`PUT ?uploadId&partNumber` + `X-Amz-Copy-Source`) is likewise rejected (`s3_communicator.go`, issues #912/#913 bucket-config, #914/#915 object-level, #920 remaining). The explicit `501 NotImplemented` SSE-KMS path (`validateSSEHeaders`, issue #776) is unchanged; SSE-C is rejected up-front with `400 InvalidRequest` and unknown SSE algorithms with `400 InvalidArgument` rather than honored.

## Known Issues and Considerations

-   **Filesystem Performance:** The performance of file I/O can vary significantly between different operating systems and underlying filesystems.
-   **Networking Stack:** The behavior of the networking stack, particularly regarding TCP congestion control and error handling, can differ between kernels. The polymorphic system in Momo is designed to adapt to these variations, but extreme network conditions may expose platform-specific behavior.
-   **Windows (WSL):** When running under WSL 2, network performance and file I/O may not match bare-metal Linux performance. It is recommended to store the data directories within the Linux filesystem (`/`) rather than accessing them across the Windows mount point (`/mnt/c`).
