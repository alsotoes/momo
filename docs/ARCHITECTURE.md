# Momo Architecture

This document provides a high-level overview of the Momo architecture, its components, and the replication strategies it supports.

## System Overview

Momo is a polymorphic file replication system that consists of a client and a cluster of servers. The client sends files to the servers, and the servers replicate the files to each other based on a configured replication strategy. The system supports multiple transport protocols (TCP, QUIC, and S3-compatible REST) and is designed to be polymorphic, meaning it can change its replication strategy at runtime based on system metrics.

### Components

The system is organized into a three-layer architecture to ensure clean separation of concerns and a pluggable transport mechanism:

#### 1. Communication Layer (Transport & App Protocol)
This layer handles the physical movement of bytes. It includes the carrier transport (e.g., TCP or UDP) and application-level framing.
- **Momo-TCP**: The legacy standard transport.
- **Momo-QUIC**: The modern, secure-by-default transport using `quic-go`.
- **S3 Compatibility**: An S3-compatible application layer (over TCP or QUIC) acting as a distributed gateway for cloud-native tools. This leverages the decoupled architecture from [Issue #131](https://github.com/alsotoes/momo/issues/131) and is tracked in [Issue #133](https://github.com/alsotoes/momo/issues/133).
  - **Custom Lightweight Gateway Design & Rationale (Issue #225)**: Instead of integrating a third-party open-source S3 server engine (such as MinIO or GoFakeS3) which would introduce dozens of heavy dependency packages and violate Momo's performance (**⚡ Bolt**) and security (**🛡️ Sentinel**) paradigms, Momo implements its own zero-dependency S3 REST adapter. This maintains a small binary surface, protects against third-party supply-chain vulnerabilities, and integrates seamlessly with both standard TCP sockets and secure **QUIC streams**.
  - **REST Query Interception Model**: Standard S3 REST commands like `GET /` (ListObjectsV2), `GET /key` (GetObject), and `DELETE /key` (DeleteObject) are intercepted directly at the HTTP layer inside `S3Communicator.HandshakeServer`.
  - **Zero-Bypass Control Flow**: Upon receiving a REST command, `S3Communicator` queries or updates our BoltDB storage layer directly and streams the S3-compliant HTTP response straight back to the client socket. It then returns a special `ErrRequestHandled` transport sentinel to the server daemon, allowing the daemon to gracefully close the connection without triggering unnecessary inter-node replication loops or replication acknowledgements (ACKs).
  - **High-Performance XML Serialization**: S3 XML responses (such as `<ListBucketResult>`) are formatted manually using `bytes.Buffer` and custom character escape sequences rather than slow, reflection-based XML serialization. This ensures sub-millisecond listing responses and conforms perfectly with the low-allocation **⚡ Bolt** standard.
- All communication is abstracted through a `Communicator` interface provided by the `ProtocolFactory`.
- **Connection Idle Timeout & POSIX Mapping**: To defend against Slowloris attacks and resource-exhaustion conditions, both inbound and outbound network connections are wrapped with an `IdleTimeoutConn` that enforces a rolling idle deadline. In alignment with POSIX standards, any socket read or write operation that times out is intercepted and wrapped to explicitly propagate a `syscall.ETIMEDOUT` error.

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain`, `Splay`). This logic is **completely agnostic** of the communication layer. It executes replication by requesting a connection (`Communicator`) from the factory and doesn't care whether bytes move via TCP or QUIC streams.

#### 3. State Management (Polymorphic System)
The metrics controller (`metrics.GetMetrics`, `src/metrics/metrics.go`) runs on the **primary (controller) node — daemon 0**; it short-circuits on any node with `serverId != 0`. It is responsible for monitoring system metrics (CPU and memory usage). When a threshold is reached, the controller broadcasts the new replication strategy to the entire cluster via the `ChangeReplication` endpoint, ensuring all potential "Primary" nodes remain in sync. (Per-node Prometheus `/metrics`/`/health` export is independent and runs on every node; see Observability §7.)

### 4. Distributed Object Engine (CAS 2.0)
Momo utilizes a **Shared-Nothing Partitioned Architecture** for its object storage layer, encapsulated in the `src/storage` package:

- **Data Placement (CRUSH)**: We use a simplified Go implementation of the **CRUSH** (Controlled Replication Under Scalable Hashing) algorithm, originally designed by **Sage Weil** (the creator of Ceph). CRUSH allows us to calculate data locations deterministically, eliminating the need for a central metadata server or coordinator. Given a file hash and the cluster map, both the client and all nodes can calculate exactly which nodes should store the data.
- **Pluggable Blob Storage**: Raw blob bytes are stored via a `BlobStore` interface (`blobstore.go`) with a `StorageFactory` (`factory.go`) that selects the backend via `[storage] backend` config. Backends: `local` (filesystem, default), `nfs`, `s3` (zero-dep SigV4 client), `raw` (block device with bump allocator).
- **Metadata Management (Bbolt)**: High-speed, transactional metadata is stored in local Bbolt databases on each node (`storage.go`). Metadata is partitioned across the cluster using the same algorithmic placement as the data itself.
- **Automatic Deduplication**: By using content-addressing (SHA-256), Momo ensures that any specific piece of data is only stored once per node, regardless of the filenames associated with it.
- **End-to-End Encryption + Confidential Dedup**: With `encryption_enabled`, content is encrypted client-side with AES-GCM-256 (streaming, chunk-bounded memory) before storage/transfer, so servers are zero-knowledge. Dedup keys stay `H(plaintext)` via a **threshold OPRF** (`oprf_enabled`): the client blinds the dedup tag, a quorum of daemons evaluates it with Shamir-split shares (over the P2P transport), and the client unblinds to derive a deterministic content key. Identical plaintexts deduplicate across tenants while no single server can derive a content key offline; the operation fails closed when the quorum is unmet.
- **Garbage Collection & Tombstones**: The `src/storage/gc.go` module implements reference-counted garbage collection with tombstone retention. When an object's refcount drops to zero, a tombstone is written with a configurable retention period (`tombstone_retention`, default 86400s). Tombstones are propagated across the cluster via P2P delete messages. GC runs periodically (`gc_interval`, default 300s) and reaps expired tombstones. See [P2P.md](P2P.md) for details on delete propagation.

### 4b. P2P Subsystem (Gossip & SWIM)
Momo includes a fully decentralized P2P subsystem (`src/p2p/`) for cluster membership, failure detection, and coordinated operations. See [P2P.md](P2P.md) for the complete protocol specification.

- **Gossip Membership**: Each node maintains a peer table and exchanges heartbeat messages containing peer state (ALIVE/SUSPECT/OFFLINE) via the gossip protocol. Heartbeats carry up to `MaxPeersInHeartbeat=256` peer entries per message.
- **SWIM Failure Detection**: Direct ping/ack probes with indirect ping (asking K random peers to probe the target) and adaptive RTT-based timeouts. Suspect marking is based on target ack timeout, not helper contact success.
- **Lease Consensus**: Lease-based consensus for coordinated operations. Leases require a quorum of `peerCount/2 + 1` peers and expire after a configurable timeout.
- **Scatter-Gather Queries**: Decentralized query mechanism for list/get/has/delete operations across the cluster without a central coordinator.
- **Quality-Aware Quorum Selection** (issue #823): scatter-gather, lease, and threshold-OPRF quorums are built from alive peers ranked by per-peer EWMA RTT (`PeerMap.AliveByQuality`), preferring low-latency members and excluding suspect/offline peers; no wire-format changes.
- **Transport**: P2P communication runs on a separate port (default `4450`, configurable via `gossip_port`). The P2P transport supports both TCP and QUIC underlying connections. P2P supports **optional TLS encryption** (mutual TLS via `[p2p] tls_cert_file`/`tls_key_file`/`tls_ca_file`) and always enforces **peer ID authentication** via an `AuthFunc` that validates connecting peer IDs against the configured daemon set, preventing spoofing and injection.

### 4c. S3 Gateway Server-Side Encryption (SSE) Boundary
The S3 gateway (`S3Communicator.validateSSEHeaders`, issues #776 / #820 P1)
enforces an explicit, honest SSE boundary rather than silently downgrading or
misrepresenting guarantees:

- **At rest, momo AES-256-GCM for everything**: the `EncryptedBlobStore`
  decorator wraps the chosen blob backend with AES-GCM-256 whenever
  `encryption_enabled` is set (`storage/factory.go`). This encrypts **every**
  object at rest, independent of any S3 SSE header — so all S3 objects are
  already protected by a real at-rest cipher. It is **opt-in** (gated on the
  at-rest key); when disabled, momo stores plaintext blobs.
- **SSE-S3 (`x-amz-server-side-encryption: AES256`)**: accepted, persisted in
  `S3Headers`, and echoed on GET/HEAD. This is an accurate claim — the object
  is encrypted at rest (momo AES-256-GCM envelope).
- **SSE-C (customer-provided key)**: rejected with `400 InvalidRequest`. Momo
  never accepts, stores, or retrieves with a customer key; faking per-object
  client-key encryption would collide with content-addressed dedup (CAS keys on
  plaintext SHA-256) and is not modeled.
- **SSE-KMS (`aws:kms` / `x-amz-server-side-encryption-aws-kms-key-id`)**:
  rejected with `501 NotImplemented`. Momo has no AWS KMS integration; reporting
  `aws:kms` while using a local envelope would break audit expectations, so it
  is never faked.
- **No fake guarantees**: every SSE-C/SSE-KMS rejection body states momo's real
  at-rest guarantee and points to SSE-S3 or client-side encryption. Clients that
  require SSE-C/SSE-KMS must use those enforcement points upstream; momo will
  not silently accept what it cannot truthfully provide.

This is a deliberate scope decision (see #820 tier P1): keep the honest
reject-and-document posture, never fake a crypto guarantee.

### 4d. Centralized Integrity Verification (protocol-agnostic)
Additive integrity checksums are verified in the **storage/ingest core**, not in
any surface protocol (issue #903), so no client protocol is a lock-in:

- **Data model**: `FileMetadata.Checksums []ChecksumRef{Algorithm, Value}`
  (`common/checksum.go`) carries additive checksums alongside the authoritative
  SHA-256 `Hash` (which remains the content-address — checksums are never
  independently addressable).
- **Central verifier**: the shared ingest path `getFile` (`server/file.go`)
  computes the requested checksums in the same bounded-memory pass as SHA-256
  (via `common.ChecksumSet`) and rejects a mismatch by deleting the object.
  Surfaces expose expectations through the protocol-agnostic
  `transport.ChecksumProvider` interface — S3 maps `x-amz-checksum-*` onto it;
  native transports have none and stay inert.
- **Replication re-verify**: forwarded S3 objects carry their checksums via the
  additive `X-Momo-S3-Meta` envelope; the receiving S3 peer re-derives
  expectations and the same central verifier re-checks at every hop.
- **Retrieval bit-rot (opt-in)**: `CASStore.VerifyChecksum(name, refs)` streams
  the stored blob and verifies the expected checksums, enabling stale/bit-rot
  detection on demand without taxing the default `Get` hot path.

### 4e. S3 Unsupported Subresource Handling (honest `501`)
S3 requests carrying a subresource param for a feature momo does not implement
are **not** silently misrouted. `S3Communicator.HandshakeServer`
(`src/transport/s3_communicator.go`) intercepts a known-but-unsupported
subresource set at the dispatch root — before any store/method routing — and
returns a clean S3-compliant `501 NotImplemented` (bounded write,
store-independent). Two sets are tracked separately, following the honest
reject-and-document posture (same philosophy as §4c):

- **Bucket-config (`key == ""`), issue #820 P3 / #912 (+ #920 P5)**: `?versioning`,
  `?versions`, `?acl`, `?policy`, `?cors`, `?website`, `?lifecycle`,
  `?tagging`, `?encryption`, `?publicAccessBlock`, `?accelerate`,
  `?replication`, `?requestPayment`, `?logging`, `?object-lock`,
  `?notification`, `?analytics`, `?inventory`, `?metrics`,
  `?intelligent-tiering`.
- **Object-level (`key != ""`), issue #820 P4 / #914 (+ #920 P5)**:
  `?tagging`, `?acl`, `?versionId`, `?retention`, `?legal-hold`, `?select`
  (SelectObjectContent).

Supported subresources are untouched: bucket `?location`, list
(`list-type` + pagination), multipart (`uploads`, `uploadId`, `partNumber`),
and batch `?delete` continue to route as before. **UploadPartCopy** (`PUT`
with `?uploadId` + `?partNumber` and an `X-Amz-Copy-Source` header, issue #920)
is intercepted separately in the PUT dispatch — before the UploadPart handler
misreads the copy source as a part body — and also returns `501 NotImplemented`.
Remaining non-subresource gaps that do not arrive via a query param (e.g.
aws-chunked trailing-checksum form, `SelectObjectContent`'s non-S3 payload
semantics) are handled by their own paths and not covered here.

### 4f. At-Rest Integrity (content-address re-verification, issue #924)
Blobs are content-addressed by SHA-256, but the read path used to stream blob
bytes out without ever re-deriving the hash — a corrupted blob at rest was
silently served. `src/storage/integrity.go` closes this with two mechanisms
(config section `[storage]`):

- **Verify-on-read (`verify_on_read`, default `true`)**: `CASStore.Get` wraps the
  blob stream in a reader that recomputes SHA-256 and, at EOF, asserts it equals
  the content-hash key. On mismatch the read fails with
  `common.ErrIntegrityMismatch` + `syscall.EBADMSG` — corrupt bytes are never
  served. Bounded-memory and computed as the caller drains the stream, outside
  the bbolt/`s.mu` critical section.
- **Background scrub (`scrub_interval`, default `3600`s)**: `StartScrub` mirrors
  the GC loop (`gcOnce`/`gcDone`/`gcWG`). Each pass lists referenced blobs from
  the `objects` bucket, re-reads and re-hashes each via `BlobStore.GetBlob` (I/O
  outside `s.mu`), and quarantines a blob whose recomputed hash no longer matches
  its key — deleting content + metadata so later reads fail explicitly with
  `ENOENT` rather than serving garbage. The helper `common.HashReader` streams a
  fixed-buffer SHA-256 (mirrors `common.HashFile`).

Re-replication/healing of quarantined blobs is out of scope here (R2, #930,
below).

### 4g. Degraded Read + Self-Heal Rebuild (R2, issue #930)
R2 adds two mechanisms over the integrity core, both behind a single Rule 74
compile-time seam (`storage.RebuildSource`) so the storage core stays network-
free:

- **Survivor-set degraded read (`degraded_read`, default `true`)**: when
  `CASStore.Get` finds a blob missing or quarantine-marked locally, it serves the
  first verified survivor replica from the placement (verify-before-store) and
  materializes the local copy (repair-on-read); it returns `ENOENT` only when no
  verified survivor exists. A verified read whose bytes fail integrity at EOF is
  mark-and-held so the next read degrades and the loop re-repairs.
- **Self-heal rebuild (`rebuild_interval` default `300`s,
  `rebuild_workers` default `4`)**: `StartRebuild`/`rebuildLoop` mirrors the
  scrub/GC loop (`rebuildOnce`/`rebuildDone`/`rebuildWG`, panic-recovered,
  goleak-safe, cancel-on-Close). Each pass iterates referenced + quarantine-
  marked blobs with a bounded worker pool and re-replicates to the target replica
  count from a verified survivor, preferring R1 failure-domain spread. Verify-
  before-use guarantees corrupt bytes are never stored or propagated (R2-C4);
  a mark-and-hold copy is replaced once verified bytes land (R2-C2). Repairs are
  counted by `RepairCount()` for the metrics tier.
- The transport conversation (CRUSH placement, peer fetch, replication push) is
  the daemon layer's implementation of `RebuildSource`, wired via
  `storage.NewStoreWithRebuild`. Without a wired source, R2 behavior is inert and
  single-node semantics are unchanged.

### 5. Automated Governance & AI Reviewer
To maintain high integrity in a single-contributor environment, Momo employs an automated governance layer:
- **Gemini AI Reviewer**: A GitHub Action that uses the Gemini API to analyze PR diffs. It specifically enforces the **⚡ Bolt** (performance) and **🛡️ Sentinel** (security) patterns.
- **Project Steering Rules**: Mandatory mandates (Zero-Crash, POSIX Error Mapping) are codified in the `context` section of `openspec/config.yaml` and automatically validated by the AI Reviewer.

### 6. Verification & Quality Assurance
The system is backed by a multi-stage automated testing pipeline:
- **Distributed Simulation**: End-to-end smoke tests simulate various cluster sizes (up to 5 nodes) and protocols.
- **Placement Validation**: Automated checks verify that the CRUSH algorithm distributes data correctly and respects the `replication_factor`.
- **Integrity Checks**: Every test suite verifies data consistency and metadata accuracy across all participating nodes.
- **Contract Tests**: Wire protocol contract tests verify handshake framing (84 bytes), metadata framing (192 bytes), round-trip integrity, and RPC framing.
- **Prometheus Metrics E2E**: Automated test starts a node, uploads a file, scrapes `/metrics`, and verifies Prometheus format + counter increments.
- **Security Pentest**: DotDotPwn fuzzing (5526 traversal patterns) + Python exploit toolkit against S3 and native TCP protocols. 9 CVEs found (1 critical, 4 high, 3 medium, 1 low). See [pentest/README.md](../pentest/README.md).

### 7. Observability (Prometheus Metrics Exporter)
Momo includes a built-in Prometheus metrics exporter (`src/server/metrics_exporter.go`) that runs as a separate goroutine on a configurable port. No external dependencies — all counters use `sync/atomic` on integer types.

**Architecture:**
- `MetricsCollector` struct holds `atomic.Uint64`/`atomic.Int64` counters — ~5ns per increment, no locks, no allocations.
- `MetricsHook` interface (defined in `src/transport/communicator.go`) is injected into each `Communicator` via `SetMetricsHook`, enabling transport-layer instrumentation for downloads, deletes, and errors that bypass the server daemon's main handler (e.g., S3 GET/DELETE returning `ErrRequestHandled`).
- `MetricsCollector` is also used directly in `server.go` for connection, upload, replication, and error counters on the main request path.
- Runtime gauges (goroutines, memory, GC runs) are computed only at scrape time via `runtime.ReadMemStats` — zero per-request overhead.

**Currently exported metrics (15):**

| Metric | Type | Description |
|---|---|---|
| `momo_connections_total` | counter | Total connections accepted |
| `momo_active_connections` | gauge | Current active connections |
| `momo_uploads_total` | counter | Total file uploads |
| `momo_downloads_total` | counter | Total file downloads |
| `momo_deletes_total` | counter | Total file deletes |
| `momo_replication_total` | counter | Total replication operations |
| `momo_errors_total` | counter | Total errors (all error paths) |
| `momo_bytes_uploaded_total` | counter | Total bytes uploaded (excludes dedup hits) |
| `momo_bytes_downloaded_total` | counter | Total bytes downloaded |
| `momo_uptime_seconds` | gauge | Server uptime in seconds |
| `momo_goroutines` | gauge | Current goroutine count |
| `momo_memory_alloc_bytes` | gauge | Allocated memory in bytes |
| `momo_memory_sys_bytes` | gauge | System memory in bytes |
| `momo_gc_runs_total` | counter | Total GC runs |
| `momo_build_info{hostname}` | gauge | Build info with hostname label |

**Metrics not yet implemented (planned for Phase 2-4):**

| Category | Metrics | Phase | Priority |
|---|---|---|---|
| **Storage** | `momo_disk_used_bytes`, `momo_disk_free_bytes`, `momo_blob_count`, `momo_stored_bytes_total` | 2 | High |
| **CAS** | `momo_dedup_hits_total`, `momo_cas_gc_runs_total`, `momo_cas_gc_evicted_bytes` | 2 | Medium |
| **Replication** | `momo_replication_bytes_total`, `momo_replication_failures_total`, `momo_replication_latency_seconds` | 3 | High |
| **P2P** | `momo_cluster_peers`, `momo_swim_alive_count`, `momo_swim_suspect_count`, `momo_swim_ping_latency_seconds` | 3 | Medium |
| **Leases** | `momo_leases_active`, `momo_lease_contentions_total` | 3 | Low |
| **Scatter/Gather** | `momo_scatter_queries_total`, `momo_scatter_timeout_total` | 3 | Low |
| **Latency Histograms** | `momo_request_latency_seconds{operation}`, `momo_replication_latency_seconds` | 4 | Medium (opt-in) |

**Configuration:** Add `prometheus_port = 9100` to the `[metrics]` section of `momo.conf`. Set to `0` or omit to disable. The metrics server runs on a separate port from the data plane — it does not share the accept loop, connection pool, or semaphore with the main daemon.

**Per-node bind:** each server process starts its own `/metrics` endpoint (`server.Daemon` → `StartMetricsServer`). By default all nodes bind `:prometheus_port` (all interfaces); in same-host/co-located topologies this collides (`EADDRINUSE`). A daemon may opt into a distinct bind via `[daemon.N] metrics_host` / `metrics_port`, which override the global `[metrics] prometheus_bind_host` / `prometheus_port` for that node. This keeps `/metrics` available on every node and lets operators scope the endpoint to an admin/mgmt interface.

**Overhead guarantees:** All counters use `sync/atomic` (~5ns per op). Heavy operations (`runtime.ReadMemStats`, disk stats) run only at scrape time (every 15-60s). No `prometheus/client_golang` dependency. Target: <1% throughput regression.

## High-Level Architecture

The system uses Sage Weil's **CRUSH algorithm** (simplified Go implementation) to distribute load across all available nodes. There is no single "entry point" or central coordinator; instead, the client deterministically selects the optimal primary node for each object based on its content hash.

```
                         +--------------------------+
                         |          Client          |
                         | (Calculates CRUSH Map)   |
                         +------------+-------------+
                                      |
                +---------------------+---------------------+
                |                     |                     |
                v                     v                     v
         +------+------+       +------+------+       +------+------+
         |   Server A  |       |   Server B  |       |   Server C  |
         | (Local Bbolt)|       | (Local Bbolt)|       | (Local Bbolt)|
         +------+------+       +------+------+       +------+------+
                |                     |                     |
                +----------+----------+----------+----------+
                           |                     |
                           v                     v
                    Replication (Agnostic of Transport)
```

**Data Flow:**

1.  **Placement Calculation**: The client hashes the file content (SHA-256) and runs the CRUSH-lite algorithm against its local Cluster Map and the configured **`replication_factor`**.
2.  **Primary Selection**: The algorithm returns an ordered list of `n` nodes (where `n = min(factor, nodes)`). The first node is the **Primary** for this specific object.
3.  **Negotiated Transfer**: The client performs an 84-byte handshake with the Primary, providing the Content Hash, Timestamp, and the intended replication mode.
4.  **Deduplication Check**: The Primary queries its local **Bbolt** instance. If the hash exists, it signals the client to skip the payload.
5.  **Algorithmic Replication**: If needed, the Primary forwards the data to the subsequent nodes in the CRUSH list (the **Secondaries**), continuing until the number of physical copies reaches the durability goal.

## Replication Strategies

Momo supports four different replication strategies:

### 1. No Replication

In this mode, the file is only stored on the server that receives it from the client. No replication occurs.

```
+----------------+      +----------------+
|                |      |                |
|     Client     +------>     Server 0   |
|                |      |                |
+----------------+      +----------------+
```

### 2. Chain Replication

In chain replication, the servers are organized in a chain. The client sends the file to the first server in the chain. The first server then replicates the file to the second server, the second to the third, and so on.

```
+----------------+      +----------------+      +----------------+      +----------------+
|                |      |                |      |                |      |                |
|     Client     +------>     Server 0   +------>     Server 1   +------>     Server 2   |
|                |      |                |      |                |      |                |
+----------------+      +----------------+      +----------------+      +----------------+
```

### 3. Splay Replication

In splay replication, the primary server (Server 0) sends the file to all other servers in the cluster simultaneously.

```
                            +------>     Server 1
                           /
+----------------+      +----------------+
|                |      |                |
|     Client     +------>     Server 0   +------>     Server 2
|                |      |                |
+----------------+      +----------------+
                           \
                            +------>      ...
```

### 4. Primary-Splay Replication

In this mode, the client sends the file to all servers in the cluster simultaneously, distributing the replication load.

```
                            +------>     Server 0
                           /
+----------------+      +---------->     Server 1
|                |     /
|     Client     +----+
|                |     \
+----------------+      +---------->     Server 2
                           \
                            +------>      ...
```

## Pluggable Storage Backend

Momo's storage layer uses a two-layer architecture:

### BlobStore (Pluggable)
Raw blob bytes keyed by content hash. The backend is selected via `[storage] backend` config field:

| Backend | Description |
|---------|-------------|
| `local` (default) | Local filesystem with tiered directory layout (`blobs/ab/cd/ef/<hash>`) |
| `nfs` | Local filesystem on an NFS mount (functionally identical to `local`) |
| `s3` | S3-compatible API via zero-dependency SigV4 HTTP client |
| `raw` | Raw block device with bump allocator and bbolt allocation table |

A `StorageFactory` (`NewStore`) mirrors the transport `ProtocolFactory`, switching on the configured backend. All backends implement the `BlobStore` interface (`PutBlob`/`GetBlob`/`DeleteBlob`).

### MetadataStore (Fixed)
Per-node bbolt metadata database (`momo.db`) handles:
- **Namespace mapping**: full virtual path → content hash
- **Reference counting**: deduplication with refcount tracking
- **Tombstones**: deletion tracking with retention-based GC
- **P2P exchange**: tombstone propagation via scatter-gather

The metadata layer is always local (bbolt in `daemon.data`), regardless of blob backend. This keeps GC, refcounting, and P2P tombstone exchange logic unchanged for all backends.

### Streaming Replication Forward
Replication forwarding (Chain/Splay) uses `store.Get()` → `connectToPeerStream(io.Reader)` to stream blobs to peers. This is backend-agnostic — no local filesystem path is required, enabling S3 and raw device backends to forward blobs seamlessly.

## Polymorphic System: Dual-Dimensional Adaptability

The defining feature of Momo is its **Dual-Dimensional Polymorphic Architecture**, which enables the system to adapt dynamically to load conditions and traffic origins with **zero manual configuration changes and zero runtime impact**:

### 📈 Dimension 1: Dynamic Replication Polymorphism (Runtime Adaptation)
The polymorphic controller (`metrics.GetMetrics`, `src/metrics/metrics.go`) runs on the primary (controller) node — **daemon 0** — and continuously samples that node's local CPU and Memory metrics. (This is distinct from the per-node Prometheus `/metrics` exporter, which runs on every server node; see Observability §7.)
- **Under Surge Load:** If system metrics exceed specified thresholds (e.g., 80% usage), the controller shifts the cluster replication mode to a lower-overhead strategy (such as **No Replication** or **Primary-Splay**) to prevent bottleneck queues and protect cluster stability, then broadcasts the change cluster-wide via `ChangeReplication`. An optional `minimum_durability_factor` (issue #822) caps this degradation: the controller refuses to auto-select a mode whose achievable replica count (≈ `min(replication_factor, daemons)`, `1` for `ReplicationNone`) is below the floor, holding the current higher-durability mode and logging the refusal rather than silently losing durability.
- **Under Low Load:** When resource usage settles below thresholds (e.g., 20% usage), the system automatically promotes the mode to highly consistent, durable strategies (like **Chain** or **Splay**), optimizing data safety.
- **Decentralized Execution:** This state change is broadcast dynamically to all potential "Primary" nodes via the `ChangeReplication` endpoint, keeping the cluster seamlessly in sync without a single point of failure.

### 🔌 Dimension 2: Wire Protocol Polymorphism (Chameleon Routing)
Momo servers listen on the exact same port (e.g., `4440`) and accept standard TCP connections or secure QUIC streams, adapting the wire framing dynamically depending on the incoming client structure:
- **Standard S3 Clients (`aws-cli`, `boto3`):** Momo acts as a pure, standard-compliant S3/Ceph REST gateway. The communicator intercepts REST operations (`GET`, `DELETE`), processes the database operations, and streams standard S3 HTTP/XML data directly back to the client socket, gracefully exiting the session using the `ErrRequestHandled` sentinel without running any custom inter-node replication procedures.
- **Momo Peer Nodes (Inter-Node Replication):** Momo acts as a highly synchronized, transactional replication engine. It detects custom handshake headers (`X-Momo-Requested-Mode`, `X-Momo-Timestamp`) inside `PUT` writes, executes our multi-stage replication framing (deduplication check, metadata verification, cluster-wide payload streaming), and transmits replication acknowledgements (`ACK` packets).

This dual-dimensional polymorphism permits Momo to simultaneously serve cloud-native clients and peer replication rings over a single port, delivering top-tier performance (**⚡ Bolt**) and robust security (**🛡️ Sentinel**) dynamically.
