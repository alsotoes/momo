# Momo Architecture

All architectural decisions are documented as **Architecture Decision Records (ADRs)** in [`docs/adr/`](adr/README.md). Each ADR corresponds to an OpenSpec change under `openspec/changes/`.

---

This document provides a high-level overview of the Momo architecture, its components, and the replication strategies it supports.

## Guiding Architectural Principles

Momo's architecture is rooted in two foundational design documents:

- **[14 Core Design Principles](CORE/DESIGN_PRINCIPLES.md)**: Centered on **Read From Any Node** (making the cluster appear as a unified system without central metadata coordination), zero single point of failure (Zero SPOF), customer-transparent complexity, self-healing, and cloud/HPC readiness.
- **[16 Biological Adaptive Models](CORE/ADAPTIVE_SYSTEMS.md)**: Stigmergy (complex global order via simple local rules), Ant Colony optimization (pheromone-based replica routing), Epigenetics (boot-time host resource sensing), and Homeostasis (dynamic load shedding under memory/CPU pressure).
- **[Engineering Standards (⚡ Bolt & 🛡️ Sentinel)](CORE/STANDARDS.md)**: Sub-microsecond zero-allocation paths and zero-trust defensive security boundaries.
- **[Architectural Decisions (DD-1 to DD-6)](CORE/DESIGN_DECISIONS.md)**: Formal trade-off evaluations including embedded BoltDB, dual independent rings, and compile-time wrapper seams.

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

### Cluster/Protocol Separation Contract (Enforced)

The architecture enforces separation through three compile-time interfaces that form hard boundaries between protocol implementations and cluster logic:

| Interface | Package | Implemented By | Consumed By |
|---|---|---|---|
| `transport.Communicator` | `transport` | `MomoTCPCommunicator`, `MomoQUICCommunicator`, `S3Communicator` | `server.Daemon`, `client.Connect` |
| `p2p.Transport` | `p2p` | `TCPTransport` | `server.bootstrapP2P`, `p2p.ScatterGather`, `p2p.LeaseManager`, `p2p.OPRFProvider` |
| `storage.Store` | `storage` | `CASStore` | `server.Daemon`, `DaemonRebuildSource`, `R6 metadata RPC` |

**Invariant:** Cluster logic (`server`, `common`, `p2p`, `storage`, `R2 rebuild`, `R6 metadata`) **never imports protocol implementations** and **never performs type assertions** on `Communicator` beyond the optional capability interfaces (`GlobalLister`, `LeaseAcquirer`, `DeletePropagator`, `MetricsHook`, `LatencyRecorder`, `OPRFService`, `ChecksumProvider`).

The `ProtocolFactory` (`transport/factory.go`) is the **sole protocol-aware component** — it dispatches based on config and returns the interface.

---

### 3. State Management (Polymorphic System)
The metrics controller (`metrics.GetMetrics`, `src/metrics/metrics.go`) runs on the **controller node**, which is whichever node holds the `coordination/metrics-controller` lease when `[coordination] enabled=true` (#1144); with election disabled it falls back to daemon 0. It monitors system metrics (CPU and memory), and when a threshold is reached it broadcasts the new replication strategy through the `ChangeReplication` endpoint.

**Any node that applies a change propagates it** (#1144 Phase 3): node 0 is not the only origin. Loop-freedom comes from timestamp ordering — a node ignores a mode update whose timestamp is not newer than the one it already applied, so it never re-broadcasts a duplicate. A zero timestamp keeps the legacy unconditional behaviour for manual/administrative overrides. (Per-node Prometheus `/metrics`/`/health` export is independent and runs on every node; see Observability §7.)

### 4. Distributed Object Engine (CAS 2.0)
Momo utilizes a **Shared-Nothing Partitioned Architecture** for its object storage layer, encapsulated in the `src/storage` package:

- **Data Placement (CRUSH)**: We use a simplified Go implementation of the **CRUSH** (Controlled Replication Under Scalable Hashing) algorithm, originally designed by **Sage Weil** (the creator of Ceph). CRUSH allows us to calculate data locations deterministically, eliminating the need for a central metadata server or coordinator. Given a file hash and the cluster map, both the client and all nodes can calculate exactly which nodes should store the data.
- **Pluggable Blob Storage**: Raw blob bytes are stored via a `BlobStore` interface (`blobstore.go`) with a `StorageFactory` (`factory.go`) that selects the backend via `[storage] backend` config. Backends: `local` (filesystem, default), `nfs`, `s3` (zero-dep SigV4 client), `raw` (block device with bump allocator), `volume` (append-only superblocks), and `adaptive` (volume for small blobs, local for large ones).
- **Adaptive Volume Layout** (`backend = volume` or `adaptive`): many small objects are packed into append-only *volume superblocks* (1 GiB default, `volume_size`) as SeaweedFS-style needles: `magic \`MOMO\` | 4-byte length | 32-byte SHA-256 | 2-byte flags | payload | CRC32C`, 8-byte aligned, so a read is one `pread` at a known offset (O(1) regardless of how many blobs the volume holds). Deletion hole-punches the extent (`FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE`) and falls back to a tombstone where the filesystem does not support it; a sealed volume is never appended to, and sealing is persisted in the volume metadata rather than inferred from file size, so restore reopens it read-only. `adaptive` routes blobs at or below `adaptive_threshold` (1 MiB default) to volumes and larger ones to per-blob files, reading across both on a miss. Volume identifiers come from CRUSH placement of the blob hash, so the layout keeps the "read from any node" property. Because small-blob workloads dominate object stores, this trades a few hundred bytes of padding per needle for far fewer inodes and seeks.
- **Metadata Management (Bbolt)**: High-speed, transactional metadata is stored in local Bbolt databases on each node (`storage.go`). Metadata is partitioned across the cluster using the same algorithmic placement as the data itself.
- **Automatic Deduplication**: By using content-addressing (SHA-256), Momo ensures that any specific piece of data is only stored once per node, regardless of the filenames associated with it.
- **End-to-End Encryption + Confidential Dedup**: With `encryption_enabled`, content is encrypted client-side with AES-GCM-256 (streaming, chunk-bounded memory) before storage/transfer, so servers are zero-knowledge. Dedup keys stay `H(plaintext)` via a **threshold OPRF** (`oprf_enabled`): the client blinds the dedup tag, a quorum of daemons evaluates it with Shamir-split shares (over the P2P transport), and the client unblinds to derive a deterministic content key. Identical plaintexts deduplicate across tenants while no single server can derive a content key offline; the operation fails closed when the quorum is unmet.
- **Garbage Collection & Tombstones**: The `src/storage/gc.go` module implements reference-counted garbage collection with tombstone retention. When an object's refcount drops to zero, a tombstone is written with a configurable retention period (`tombstone_retention`, default 86400s). Tombstones are propagated across the cluster via P2P delete messages. GC runs periodically (`gc_interval`, default 300s) and reaps expired tombstones. See [P2P.md](P2P.md) for details on delete propagation.

### 4b. P2P Subsystem (Gossip & SWIM)
Momo includes a fully decentralized P2P subsystem (`src/p2p/`) for cluster membership, failure detection, and coordinated operations. See [REFERENCE/P2P.md](REFERENCE/P2P.md) for the complete protocol specification.

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

- **Bucket-config (`key == ""`), issue #820 P3 / #912 (+ #920 P5, #938
  implemented surface removed)**: `?acl`, `?policy`, `?cors`, `?website`,
  `?tagging`, `?encryption`, `?publicAccessBlock`, `?accelerate`,
  `?replication`, `?requestPayment`, `?logging`, `?analytics`, `?inventory`,
  `?metrics`, `?intelligent-tiering`.
- **Object-level (`key != ""`), issue #820 P4 / #914 (+ #920 P5)**:
  `?tagging`, `?acl`, `?select` (SelectObjectContent).

Implemented subresources route **before** the detector and must never be added
back to these sets: bucket `?versioning`/`?versions`, `?lifecycle`,
`?notification`, `?object-lock`, object `?retention`/`?legal-hold`, and
`?versionId`. A store lacking the capability answers its own explanatory `501`
instead. A dispatch-level regression test covers each (a supported param in the
unsupported set is a silent feature outage — the `?notification` case, fixed in
#1281).

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

### 4g-bis. Stigmergic Rebalance (R11, issue #1143)
R11 balances disk usage with no coordinator: every node applies the same local
rule and the cluster converges as a side effect (stigmergy, not scheduling).

- **Disk-pressure shedding (`[rebalance]`, default disabled)**: each tick
  compares local used-disk percentage against `disk_high_water`; above it, the
  node moves its coldest blobs (oldest write time as the access proxy, capped by
  `max_shed_per_cycle`) to a peer that already holds a replica, and stops at
  `disk_low_water`.
- **Membership migration**: when a configured peer joins, blobs whose CRUSH
  placement moved to that peer and no longer includes this node are pushed and
  then dropped locally.
- **Safety invariants**: bytes leave only after the peer acknowledges them
  (`DaemonRebuildSource.Push`), placement still containing this node never sheds,
  single-node clusters never shed, and the loop refuses to start unless
  `storage.degraded_read=true` — the local read then falls back to a verified
  survivor while the rebuild loop re-establishes the replica count.
- **Streaming**: a rebalance push reports the blob's directory-entry size and
  streams it from disk through the throttled writer, so memory never scales with
  object size. Integrity is enforced at ingest and on read.
- **Throttling**: `max_bandwidth_mbps` (default `0` = unlimited) spends a token
  bucket per 64 KiB write (`writeThrottled`), so a shed or migration pass cannot
  starve foreground traffic on the same uplink.
- **Peer handshake**: rebalance and rebuild dials send a fresh anti-replay
  timestamp (issue #657). An integration test drives a real TCP push — dial,
  handshake, metadata, streamed payload, ACK — because a mocked transport cannot
  enforce the freshness contract.
- **Observability**: `momo_rebalance_blobs_moved_total`,
  `momo_rebalance_bytes_moved_total`, `momo_rebalance_cycles_total`, and
  `momo_rebalance_failures_total` are recorded at the push choke point, so they
  count bytes that actually left the node.
- **Metadata is not presence**: a shed blob keeps metadata, so `Put` asks the
  backend (`blobPresence`) whether the bytes are physically present and rewrites
  them when they are not. Without this, a peer could acknowledge a push it did
  not store and the sender would drop the last copy (found by
  `TestRebalanceChaosNeverLosesTheLastCopy`).
- **Serialized passes**: one mutex covers a whole evaluation pass, so a timer
  tick and a join migration cannot push the same blob twice; the waiting pass
  re-reads the blob list afterwards.
- **Preconditions**: validated at config load (P2P enabled, `degraded_read`,
  `replication_factor >= 2`, at least two daemons) so a misconfigured loop fails
  at startup instead of never running.
- **Metadata quorum writes**: an any-node metadata write fans out to the shard's
  replica set and is acknowledged only after a strict majority
  (W = M/2 + 1) of acks, with the writer advancing its vector-clock entry first.
  Partial success is reported as an error, and replicas fail closed on nameless
  records. A send that fails is also recorded as a hint, so the replica converges
  once it returns even though the request itself still fails below quorum. The daemon's PUT path uses it whenever distributed metadata is enabled
  (`[momofs] enabled=true`, replication factor > 1); a quorum failure fails the
  request so the client retries, and the retry is idempotent. Deletes propagate
  tombstones to the same replica set (replica-applied deletions skip the hook, so
  propagation cannot loop). Reads fall back to the replica set on a local miss,
  order divergent records by vector clock, break true concurrency
  deterministically, and repair stale replicas; tombstoned names are never
  resolved, so a delete cannot resurrect from a replica.
- **Adaptive thresholds** (opt-in, `[adaptive]`): the polymorphic controller's
  memory-pressure thresholds are learned from an EWMA of observed utilisation,
  with the configured value as a floor and clamps (max 60–95%, min 20–60%, ≥10
  point gap, 5-point step) so the control loop cannot twitch. Learned values
  persist in `<data_dir>/adaptive.db` and are exported as
  `momo_adaptive_*` gauges.
- **Homeostatic connection admission**: the accept loop takes slots from a
  controller sized from the host's FD limit and cgroup/RAM ceiling (clamped
  32–10000); every 10 seconds CPU and memory pressure are sampled, shedding to
  50% above 80% and 25% above 90% so a loaded node stops accepting rather than
  thrashing.
- **Adaptive metadata RPC timeout**: 10× the cluster's measured mean RTT, clamped
  to 500 ms–10 s, so a fast cluster fails over quickly and a slow one is not
  declared dead prematurely.
- **Adaptive replication pool**: broadcasts feed their real payload size to an
  EWMA learner; the pooled buffer target follows `mean × 1.5` within
  `[1 KiB, 1 MiB]`, so the pool stops reallocating on every send without holding
  a megabyte per P for a rare case.
- **Load-aware shedding**: the gossip load summary ranks a blob's placement
  peers when choosing a shed target — healthy before unknown before saturated
  (disk ≥ 95% or ≥ 1000 in-flight), ties broken on disk then in-flight — so
  bytes are not pushed onto a node that is already struggling.
- **Mode continuity**: every node records the replication mode it observes in
  `<data>/coordination/replication_mode` (atomic write). A controller restart or
  takeover resumes that value instead of resetting to the first entry of the
  configuration order, so failover never silently downgrades durability.
- **Proactive handoff**: when `[coordination_failover]` is enabled, the node
  samples CPU, memory and goroutines; breaching a threshold resigns every held
  lease (broadcast) so a healthy peer takes over, with a cooldown against churn.
  CPU/memory are read from `/proc` on Linux and reported as unknown elsewhere,
  so those dimensions never fire on platforms that cannot observe them.
- **Split-brain prevention**: lease acquisition requires a majority of the
  configured cluster (not of the reachable subset), so a minority partition can
  never grant coordination. Losing quorum releases every held lease and audits
  the suspension; a single-node cluster is unaffected.
- **Lease failover**: SWIM degradation events (SUSPECT/OFFLINE) notify the lease
  manager, which immediately releases any held lease whose quorum is no longer
  satisfiable and broadcasts the release. Coordination therefore stops in
  seconds rather than waiting for the lease TTL, and a healthy node can take the
  role over without a coordinator.
- **Peer load gossip**: every node publishes a fixed 20-byte summary (disk
  percent, in-flight requests, reserved queue depth, timestamp) to the peers that
  receive its heartbeat, and stores the latest report per peer. The channel is
  bounded and optional (no provider = no traffic); no decision consumes it yet —
  the redirect contract is tracked in #1261.
- **Ownership**: re-replication after a leave and failure-domain spread stay with
  the R2 rebuild loop and R1 placement; R11 does not duplicate them.

### 4h. Write Durability + Consistency Model (R3, issue #931)
R3 defines the durability-and-ack contract for writes and the single-object
consistency model, both enforced in the storage core and documented here.

- **Durability barrier (`durability = "fsync" | "group-commit" | "none"`,
  default `fsync`)**: every write crosses a Rule 74 barrier before it is
  acknowledged. `fsync` fsyncs each blob's bytes (`SyncBlob`); `group-commit`
  covers a batch of atomic renames with one winner-driven directory-fsync
  barrier (`SyncDir`) and skips per-blob data fsync — amortized cost, while
  content addressing and the R2 verified-read/self-heal loop keep the
  durability + verification failure model sound (a torn block between the
  barrier and OS writeback is detected and repaired); `none` acknowledges
  buffered writes with no fsync and is explicitly non-durable. Modes are
  declaratively selected (`WithDurability`) from the compiled-in
  `durabilityRegistry`; invalid values fail at config load with `EINVAL`
  (fail-closed, never silently under-durable).
- **Write quorum (`write_quorum`, default `1`, `[1, replication_factor]`)**:
  a write is acknowledged only after `write_quorum` replicas are durably
  persisted. A write that cannot reach the quorum fails (no silent ack). The
  metrics controller's `minimum_durability_factor` floor (#822) never selects a
  mode whose achievable durable replicas fall below the quorum.
- **Consistency model (`R3-C3/R3-C4`)**: single-object operations are
  **sequential**: `CASStore` serializes writes per object (global lock + single
  transactional metadata update, so a value is never torn), the last
  acknowledged writer wins (LWW), and an acknowledged write is observed by
  subsequent reads on the same object (read-your-writes). Multi-object /
  namespace operations have **no cross-object atomicity** — each object obeys
  the model independently (no distributed transaction at this milestone).
- The multi-replica ack-counting (client waits for `write_quorum` durable acks
  across daemons) is the daemon transport layer's wiring over the storage
  barrier, delivered as a follow-up; the storage core enforces the per-node
  durable-persist half of the contract.

### 4i. momofs — POSIX layer over CAS (R4, issue #932)
`src/momofs` is the storage-backed virtual filesystem core ratifying the
`docs/momofs/` design baseline. It exposes a POSIX tree over a single
`storage.Store`:

- **Directories are content-addressed manifest blobs** — a JSON directory
  manifest (direct children with mode/uid/gid/size/mtime) stored as a store
  object, versioned by content hash. Every directory mutation rewrites the
  manifest, so directory updates are atomic.
- **Files are store objects keyed by path identity** (`f-<sha256(path)>`);
  content is deduplicated across paths via the content hash. POSIX metadata
  lives in the parent manifest.
- **Rename (R4-C2)**: file renames move the store reference between path keys
  (net refcount unchanged); directory renames relocate the subtree's
  path-keyed manifests and file references (`rewriteSubtree`), O(subtree)
  matching POSIX `mv` cost.
- **Hardlinks (R4-C2)**: a link takes a second store reference to the same
  content under the destination path key, so refcounts stay aligned with link
  counts and CAS GC never reclaims a blob with a live link.
- **S3/native ↔ mount consistency (R4-C3)**: the read path unions the
  manifests with a backing-store index, so objects written natively (S3/momo)
  appear in the mount under synthetic directories, and mount-created files are
  visible store objects. Zero TTL means manifests are re-read fresh on every
  op (read-your-writes; remounts see committed state, R4-C4).
- The **FUSE/syscall transport** (`hanwen/go-fuse/v2` binding, CLI flags `-imp fs`
  and `-fs-mount`, CI mount-e2e) is implemented (`src/momofs/fuse.go`, R4 #963).
  The mmap/posix-locks advisory surface remains a documented follow-up,
  matching the R2 `RebuildSource` / R3 barrier transport pattern.

### 4j. Distributed Metadata Catalog (R6 v2, issues #934/#1142/#1233)

Node-local BoltDB remains the fast path; the catalog adds optional quorum
durability for deployments that cannot lose a name when a node dies.

- **Placement**: 256 shards over a 150-vnode-per-node consistent hash ring
  (`src/p2p/metadata_ring.go`). Every node computes placement from the cluster
  map, so no coordinator or lookup service is involved.
- **Writes**: `MsgPutMetadata` fans out to the shard's replica set and is
  acknowledged at `W = M/2 + 1` (`metadata_replication` = M,
  `metadata_quorum` = W, majority by default). Partial success fails the request
  with `EIO` so the client retries; the retry is idempotent and advances the
  writer's vector clock.
- **Deletes**: tombstones propagate over `MsgDeleteMetadata` at the same quorum.
  Replica-applied deletions skip the local delete hook, so propagation cannot
  loop.
- **Reads**: any replica may answer. The reader picks the dominating vector
  clock, breaks genuinely concurrent clocks deterministically (newest modtime,
  then greater hash) with an `AUDIT` line, repairs stale replicas, and caches the
  winner locally. A tombstoned name is never resolved from a replica — the
  fallback would otherwise resurrect a deleted object.
- **Version records** (`MsgReplicateVersion`) follow the object's data replica
  set so version listing/get/delete and lock checks agree on every holder
  (#1233); delivery is best-effort and audited.
- **Hinted handoff** (#1142): whenever a write or delete could not be delivered
  to a replica, the coordinator keeps the encoded record in the local
  `metadata_hints` bucket keyed by peer + name (one-hour TTL, 4096 hints per
  peer, oldest pruned first). `ReplayMetadataHints` re-sends each hint when the
  peer is alive and deletes it only after the acknowledgement, so a brief outage
  does not depend on read repair or on the next write to converge. A five-minute
  sweep replays hints for peers already ALIVE, so a missed transition cannot
  strand a hint until an unrelated flap.
- **Backup/restore**: `momo backup/restore` (`--output`, `--compress`, `--force`)
  wraps BoltDB snapshot I/O for point-in-time recovery.
- **Scope**: behind `[momofs] enabled=true` + `metadata_replication > 1`. With it
  disabled every node keeps its metadata local (single-node behaviour unchanged).

### 4k. S3 Breadth (R10, issue #938)

The gateway covers the backup-tooling surface beyond basic PUT/GET:

- **Versioning** (`?versioning`, `?versions`, `?versionId`): set/suspend, list
  versions, version-addressed GET/HEAD/DELETE with delete markers. CAS dedup
  keeps N identical versions on one stored blob. Plain PUT cannot echo
  `x-amz-version-id` because the response precedes body streaming — multipart
  does.
- **Lifecycle** (`?lifecycle`): rule CRUD plus a background worker
  (`[s3] lifecycle_worker_interval`) applying prefix-scoped expiration (delete, or
  delete marker on versioned buckets) and storage-class transitions. A
  "transition" only rewrites `x-amz-storage-class`; momo has no physical tiers.
- **Notifications** (`?notification`): webhook rules delivered with exponential
  backoff, bounded by `[s3] notification_retry_max` (5xx/network retried, 4xx
  not).
- **Object Lock** (`?object-lock`, `?retention`, `?legal-hold`): creation-only
  enablement (never disableable), per-version GOVERNANCE/COMPLIANCE retention and
  legal hold, delete enforcement in the version store, governance bypass via
  `x-amz-bypass-governance-retention`; COMPLIANCE is extend-only. Ceilings:
  default-retention rules are unsupported, a single-part PUT cannot capture lock
  headers (use multipart), and lock state is S3-gateway-scoped.
- Unsupported subresources are rejected at the dispatch root with a clean
  `501 NotImplemented` rather than falling through (see §4e).

### 4l. Tenancy, Authorization & Audit (R8, issue #936)

Multi-tenancy is opt-in and layered on top of the existing CAS, without changing
the data layout:

- **Tenants** (`[tenants]` + `[tenant.<id>]`): an ID, the master-key ID it is
  scoped to, an auth token, and optional `quota_bytes` / `quota_objects`. Each
  tenant record is stored in the `tenant_meta` bucket and managed through
  `PutTenantConfig` / `GetTenantConfig` / `ListTenantConfigs` / `DeleteTenantConfig`.
  The quota fields are **parsed and stored but not enforced** — see
  `openspec/changes/r8-multitenancy-authz/tasks.md`.
- **Identity**: the S3 gateway resolves a request's tenant from its Bearer token
  or its SigV4 access key. Native TCP/QUIC clients authenticate with the cluster
  token but carry no tenant identity, so tenancy is an S3-gateway boundary; native
  operations are audited but not tenant-scoped.
- **ACLs** (`acl` bucket): bucket-level and object-level entries, with an object
  ACL overriding its bucket. Permissions are `read`, `write`, `delete`, `admin`
  over resources named `bucket/<name>` and `object/<bucket>/<key>`.
  `CASStore.Authorize(tenantID, perm, resource)` is **default-deny** with an
  `admin` bypass, and every S3 handler calls it before touching the store.
- **Audit log** (`audit_log` bucket): each entry records timestamp, tenant,
  identity, operation, resource, outcome and request ID, and chains to its
  predecessor with SHA-256 (`prev_hash` + `entry_hash`), so altering a stored
  entry breaks `VerifyAuditLog`. Entries are appended for S3
  GET/HEAD/PUT/DELETE/LIST and native LIST/GET/DELETE plus the shared PUT path.
  The log is bounded by `[audit] retention_days` (startup + daily prune, chain
  anchor, see §7b). **Tamper evidence is only as strong as the node's own
  database**: an attacker with write access can delete the log wholesale; the
  chain detects edits and the anchor makes the window boundary explicit.
- **Per-tenant keys**: `DeriveTenantKeys` (HKDF-SHA256), `WrapTenantKEK` /
  `UnwrapTenantKEK` (AES-256-GCM with the tenant ID as AAD) and `RotateTenantKEK`
  exist and are tested, but the write path still uses the node-wide at-rest key —
  per-tenant key selection is not yet wired.

### 4m. Secrets & Key Rotation (R9, issue #937)

Secret material is resolved through a compile-time seam (Rule 74) rather than
being read directly wherever it is needed:

- **`SecretProvider`** (`GetSecret`, `Init`, `Reload`, `HealthCheck`, `Close`)
  with a **`ProviderChain`** tried in the configured order. The default is
  `sources = "env, momo"`: `EnvProvider` reads `MOMO_<NAME>` (prefix configurable),
  and `FileProvider` reads `conf/momo.conf`, which keeps every existing deployment
  working with no migration.
- **External providers are declared but not implemented**: `vault`, `aws-sm`,
  `gcp-sm` and `azure-kv` are accepted by the enum and return an explicit
  "not yet implemented" error at construction. They are deliberately not faked —
  a silent fallback to env/file would hide a misconfiguration.
- **Key registry** (`keys.db`, its own bbolt file because two `bbolt.Open` calls on
  one file deadlock): versioned keys per purpose with status
  `active` / `retired` / `pending_rotation` / `compromised`; any node resolves the
  active key from its local copy, so there is no central secret server.
- **Rotation**: `RotationManager` rotates on a schedule (24 h default grace
  period), keeps the previous version usable during the grace window, and records
  both success and failure in the audit log. Rotation is triggered by `momo
  rotate-secrets -purpose`, by `SIGHUP`, or by the authenticated
  `POST /reload-secrets` admin endpoint.

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
- **Security Pentest**: DotDotPwn fuzzing (5526 traversal patterns) + Python exploit toolkit against S3 and native TCP protocols. 10 CVEs found (1 critical, 5 high, 3 medium, 1 low). See [pentest/README.md](../pentest/README.md).

### 7. Observability (Prometheus Metrics Exporter)
Replication-mode broadcasts reuse one adaptive payload buffer pool **per server
instance** (`replicationPool`): its capacity starts at 1 KiB, learns the observed
payload size, and is bounded at 1 MiB. Only buffers still at the current target
capacity return to the pool, so an oversized append cannot poison it (#1145,
#1149).

Momo includes a built-in Prometheus metrics exporter (`src/server/metrics_exporter.go`) that runs as a separate goroutine on a configurable port. No external dependencies — all counters use `sync/atomic` on integer types.

**Architecture:**
- `MetricsCollector` struct holds `atomic.Uint64`/`atomic.Int64` counters — ~5ns per increment, no locks, no allocations.
- `MetricsHook` interface (defined in `src/transport/communicator.go`) is injected into each `Communicator` via `SetMetricsHook`, enabling transport-layer instrumentation for downloads, deletes, and errors that bypass the server daemon's main handler (e.g., S3 GET/DELETE returning `ErrRequestHandled`).
- `MetricsCollector` is also used directly in `server.go` for connection, upload, replication, and error counters on the main request path.
- Runtime gauges (goroutines, memory, GC runs) are computed only at scrape time via `runtime.ReadMemStats` — zero per-request overhead.

**Currently exported metrics (47 families):**

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

**R5 storage/CAS/P2P/cluster metrics (phases 2-4, all implemented in `metrics_exporter.go`):**

| Category | Metrics | Type |
|---|---|---|
| **Storage** | `momo_disk_used_bytes`, `momo_disk_free_bytes` (Statfs), `momo_blob_count`, `momo_stored_bytes_total` | gauge |
| **CAS** | `momo_dedup_hits_total`, `momo_cas_gc_runs_total`, `momo_cas_gc_evicted_bytes` | counter |
| **Replication** | `momo_replication_bytes_total`, `momo_replication_failures_total`, `momo_replication_latency_seconds` (opt-in histogram) | counter / histogram |
| **P2P** | `momo_cluster_peers`, `momo_swim_alive_count`, `momo_swim_suspect_count`, `momo_swim_offline_count`, `momo_swim_ping_latency_seconds` (mean EWMA RTT), `momo_peer_disk_used_percent{peer}`, `momo_peer_inflight_requests{peer}` (last gossip report; absent until one arrives) | gauge |
| **Leases** | `momo_leases_active` (held), `momo_coordination_handoffs_total` (proactive resignations), `momo_coordination_quorum_loss_total` (coordination suspensions) | gauge + counters |
| **Scatter/Gather** | `momo_scatter_queries_total`, `momo_scatter_timeout_total` | counter |
| **Rebalance (R11)** | `momo_rebalance_blobs_moved_total`, `momo_rebalance_bytes_moved_total`, `momo_rebalance_cycles_total`, `momo_rebalance_failures_total` | counter |
| **Adaptive** | `momo_adaptive_max_threshold`, `momo_adaptive_min_threshold` (percent), `momo_adaptive_threshold_samples` | gauge |
| **Audit retention** | `momo_audit_prune_runs_total` (retention passes), `momo_audit_pruned_entries_total` (entries removed) | counter |
| **Latency Histograms** | `momo_request_latency_seconds{operation}` (upload/download/delete/list), `momo_replication_latency_seconds` | opt-in histogram |

The only phase 2-4 metric not yet implemented is `momo_lease_contentions_total`.
Latency histograms are gated behind `enable_latency_histograms = true` in the
`[metrics]` section and add zero overhead when disabled (no `time.Now()` /
bucket increments on the hot path).

**Configuration:** Add `prometheus_port = 9100` to the `[metrics]` section of `momo.conf`. Set to `0` or omit to disable. The metrics server runs on a separate port from the data plane — it does not share the accept loop, connection pool, or semaphore with the main daemon.

**Per-node bind:** each server process starts its own `/metrics` endpoint (`server.Daemon` → `StartMetricsServer`). By default all nodes bind `:prometheus_port` (all interfaces); in same-host/co-located topologies this collides (`EADDRINUSE`). A daemon may opt into a distinct bind via `[daemon.N] metrics_host` / `metrics_port`, which override the global `[metrics] prometheus_bind_host` / `prometheus_port` for that node. This keeps `/metrics` available on every node and lets operators scope the endpoint to an admin/mgmt interface.

**Overhead guarantees:** All counters use `sync/atomic` (~5ns per op). Heavy operations (`runtime.ReadMemStats`, disk stats) run only at scrape time (every 15-60s). No `prometheus/client_golang` dependency. Target: <1% throughput regression.

### 7b. State, Persistence, and What Survives a Restart

Momo adapts at runtime: it switches replication mode, learns CPU/memory baselines,
and can elect its controller by lease. Two rules decide where that state lives.

**`momo.conf` is operator-owned input.** The daemon only reads it (`ini.Load` in
`src/common/config.go`); nothing writes it back. Learning relaxes thresholds
*within* the configured bounds but never below the operator's floor, so a restart
cannot lose or rewrite configuration.

**Learned state is durable, per node.** Each daemon persists its own copy; none of
it is cluster-wide:

| Learned state | Store (relative to the node data dir) | Restored by |
|---|---|---|
| Replication mode index | `coordination/replication_mode` | `resumeMode` at loop start and on controller takeover |
| Adaptive CPU/memory thresholds | `adaptive.db` | `NewAdaptiveThresholds` (EWMA baseline + clamps) |
| Object metadata, versions, locks, ACLs, tenants, audit chain, metadata hints | `momo.db` | `CASStore` open |
| Secret/key versions | `keys.db` | key registry open |

**Observations are ephemeral and relearned**, deliberately without a write path:
gossip peer map and SWIM states, RTT EWMA, peer load reports, leases, the
replication buffer-pool target, client pheromone tuning, and Prometheus counters.
A restart costs convergence time (seconds), not decisions.

**The audit log is bounded by policy.** The `audit_log` bucket is hash-chained
for tamper evidence, so retention cannot just delete rows: the daemon prunes
entries older than `[audit] retention_days` at startup and then daily, and stores
the hash of the last removed entry in `audit_meta` as the new chain anchor
(#1307). `VerifyAuditLog` seeds from that anchor, so the retained window still
verifies and tampering inside it is still detected. Pruning intentionally gives
up tamper evidence for entries older than the window.

**Inspecting it:** `GET /health/detailed` on each node returns a `learned_state`
object with the election status, the resumed replication mode (plus whether it is
persisted), and the learned adaptive thresholds. It reports persistence as a
boolean plus a **relative** store name — never an absolute path, because the
metrics listener is unauthenticated. The block is omitted when no provider is
installed (embedded/test collectors).

> **Admin listener authentication (#1289).** Set `[metrics] admin_token` and every
> endpoint except `/health` requires `Authorization: Bearer <token>` (constant-time
> comparison). Without a token the mutating `POST /reload-secrets` is refused on a
> non-loopback bind (`403`), so an exposed listener can never rotate keys; the
> read-only endpoints keep their historical open behaviour for compatibility. Bind
> the listener to an admin interface anyway (`[metrics] prometheus_bind_host`,
> per-daemon `metrics_host`).

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
The polymorphic controller (`metrics.GetMetrics`, `src/metrics/metrics.go`) runs on the **controller node**: whichever node holds the `coordination/metrics-controller` lease when `[coordination] enabled=true` (#1144), otherwise daemon 0 (legacy). It samples that node's local CPU and Memory metrics. (This is distinct from the per-node Prometheus `/metrics` exporter, which runs on every server node; see Observability §7.)
- **Under Surge Load:** If system metrics exceed specified thresholds (e.g., 80% usage), the controller shifts the cluster replication mode to a lower-overhead strategy (such as **No Replication** or **Primary-Splay**) to prevent bottleneck queues and protect cluster stability, then broadcasts the change cluster-wide via `ChangeReplication`. An optional `minimum_durability_factor` (issue #822) caps this degradation: the controller refuses to auto-select a mode whose achievable replica count (≈ `min(replication_factor, daemons)`, `1` for `ReplicationNone`) is below the floor, holding the current higher-durability mode and logging the refusal rather than silently losing durability.
- **Under Low Load:** When resource usage settles below thresholds (e.g., 20% usage), the system automatically promotes the mode to highly consistent, durable strategies (like **Chain** or **Splay**), optimizing data safety.
- **Decentralized Execution:** Any node that applies a mode change forwards it to the other daemons via the `ChangeReplication` endpoint; the receiver accepts it only if the payload's timestamp is newer, so re-broadcasts cannot loop. Node 0 is not a required origin, and each node persists the mode it observed so a restart or controller takeover resumes it.

### 🔌 Dimension 2: Wire Protocol Polymorphism (Chameleon Routing)
Momo servers listen on the exact same port (e.g., `4440`) and accept standard TCP connections or secure QUIC streams, adapting the wire framing dynamically depending on the incoming client structure:
- **Standard S3 Clients (`aws-cli`, `boto3`):** Momo acts as a pure, standard-compliant S3/Ceph REST gateway. The communicator intercepts REST operations (`GET`, `DELETE`), processes the database operations, and streams standard S3 HTTP/XML data directly back to the client socket, gracefully exiting the session using the `ErrRequestHandled` sentinel without running any custom inter-node replication procedures.
- **Momo Peer Nodes (Inter-Node Replication):** Momo acts as a highly synchronized, transactional replication engine. It detects custom handshake headers (`X-Momo-Requested-Mode`, `X-Momo-Timestamp`) inside `PUT` writes, executes our multi-stage replication framing (deduplication check, metadata verification, cluster-wide payload streaming), and transmits replication acknowledgements (`ACK` packets).

This dual-dimensional polymorphism permits Momo to simultaneously serve cloud-native clients and peer replication rings over a single port, delivering top-tier performance (**⚡ Bolt**) and robust security (**🛡️ Sentinel**) dynamically.
