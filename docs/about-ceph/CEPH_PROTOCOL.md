# CEPH_PROTOCOL.md — Ceph Communication Protocol Reference

## Table of Contents
1. [Overview: Three-Tier Communication Model](#1-overview-three-tier-communication-model)
2. [Message Framing & Transport](#2-message-framing--transport)
   - [2.1 BlueRing Protocol (Intra-OSD)](#21-bluering-protocol-intra-osd)
   - [2.2 Remote Procedure (BlueBox/gRPC-style)](#22-remote-procedure-bluespc-style)
   - [2.3 MON Cluster Protocol](#23-mon-cluster-protocol)
3. [OSD Intra-Cluster Protocol](#3-osd-intra-cluster-protocol)
   - [3.1 OSD-to-OSD Replication](#31-osd-to-osd-replication)
   - [3.2 CRUSH Placement Protocol](#32-crush-placement-protocol)
   - [3.3 BlueStore WAL Protocol](#33-bluestore-wal-protocol)
4. [Metadata Protocol (Distributed Replication)](#4-metadata-protocol-distributed-replication)
   - [4.1 Shard Ownership Resolution](#41-shard-ownership-resolution)
   - [4.2 Quorum Protocol for Metadata Writes](#42-quorum-protocol-for-metadata-writes)
   - [4.3 Read Protocol with Failover](#43-read-protocol-with-failover)
5. [Gossip & SWIM Protocol](#5-gossip--swim-protocol)
   - [5.1 Purpose](#51-purpose)
   - [5.2 Message Types](#52-message-types)
   - [5.3 Gossip Algorithm](#53-gossip-algorithm)
   - [5.4 Heartbeat Integration](#54-heartbeat-integration)
   - [5.5 What Gossip Does NOT Do](#55-what-gossip-does-not-do)
   - [5.6 Comparison: Ceph Gossip vs. Other Systems](#56-comparison-ceph-gossip-vs-other-systems)
6. [S3/RGW Protocol (RGW Gateway)](#6-s3rgw-protocol-rgw-gateway)
   - [6.1 REST API over TCP](#61-rest-api-over-tcp)
   - [6.2 Authentication](#62-authentication)
   - [6.3 Response Codes](#63-response-codes)
7. [Comparison: Ceph vs. MomoFS Protocol Design](#7-comparison-ceph-vs-momofs-protocol-design)
8. [Key Protocol Design Principles](#8-key-protocol-design-principles)
9. [MomoFS Protocol Adaptations](#9-momofs-protocol-adaptations)
10. [Summary: Protocol Architecture](#10-summary-protocol-architecture)

---

## 1. Overview: Three-Tier Communication Model

Ceph's communication architecture follows a three-tier model:

| Tier | Responsibility | Key Daemons | Communication Style |
|------|---------------|-------------|---------------------|
| **Transport** | Reliable message delivery | BlueRing, TCP/IP, OSD sockets | Connection-oriented, framed messages |
| **Protocol** | Operation semantics | CRUSH, BlueStore WAL, Journal, Replication | Deterministic, quorum-based, replica coordination |
| **Application** | User-facing operations | RGW (S3), RBD (block), CephFS (POSIX) | API-specific, translates to lower tiers |

---

## 2. Message Framing & Transport

### 2.1 BlueRing Protocol (Intra-OSD)
- **Framework**: Seastar C++ framework (Crimson/SeaStore)
- **Message format**: `[length: uint32][opcode: uint8][payload: variable]`
- **Delivery**: Guaranteed within single OSD process
- **Backpressure**: Seastar's `conditional_variable` + `latch` pattern
- **Key insight**: No deserialization overhead for same-process communication

### 2.2 Remote Procedure (BlueBox/gRPC-style)
- **Framework**: Custom protocol over TCP, not gRPC (pre-2024)
- **Message format**: 
  ```
  [client_id: 8 bytes]
  [xid: 8 bytes]      # transaction ID for correlation
  [opcode: 1 byte]    # MSGR_OP_*
  [payload length: 4 bytes]
  [payload: variable]
  ```
- **Delivery**: Async via event loop; reply correlation via `xid`
- **Timeouts**: Configurable per operation (default 30s for most)
- **Retries**: Idempotent operations can retry; non-idempotent uses token-based deduplication

### 2.3 MON Cluster Protocol
- **Transport**: TCP cluster on port `3300/3301`
- **Protocol**: Paxos-like consensus for cluster map
- **Messages**: 
  - `MSGR_WANT_..` for map updates
  - `MSGR_HAVE_..` for acknowledgments
  - `MSGR_ELECTION` for leader selection
- **Quorum**: 3-5 MON daemons; majority wins
- **Purpose**: Cluster map distribution, OSD/pg/CRUSH map propagation

---

## 3. OSD Intra-Cluster Protocol

### 3.1 OSD-to-OSD Replication
```
Pattern: Primary → Secondary1 → Secondary2 → ... → Primary (final commit)
```

**Write flow**:
```
1. Client → OSD (any) → Primary OSD (via CRUSH)
2. Primary: write local → replicate to secondaries (sync RPC)
3. Secondaries: write local → ack primary
4. Primary: wait for (N-1) acks → success to client
5. Background: recovery/scrub propagates any missing replicas
```

**Read flow**:
```
1. Client → OSD (any)
2. OSD: check local → if hit, serve locally
3. OSD: miss → proxy from primary/other replica (or return error)
4. Secondary may serve reads if primary is down (with staleness window)
```

### 3.2 CRUSH Placement Protocol
- **Algorithm**: `choose_tries` — deterministic pseudo-code
- **Input**: 
  - Object hash/key
  - CRUSH map (hierarchy: root → rack → host → OSD)
  - Number of desired replicas (N)
  - Failure domain constraints (no-two-from-same-rack, etc.)
- **Output**: Ordered list of N OSD IDs
- **Key property**: Same inputs → same outputs (no central coordination needed)
- **Updates**: CRUSH map changes propagate via MON → OSDs reload → rebalancing begins

### 3.3 BlueStore WAL Protocol
**Write sequence** (from JOURNALING.md analysis):
```
1. Client → OSD: write data
2. OSD → WAL device: [sequence: uint64][operation type][data...]
3. OSD: acknowledge to client (after step 3 in full sequence)
4. OSD → main device: commit data to final location
5. OSD → WAL: remove entry after successful commit

Crash recovery on restart:
1. Scan WAL file for pending operations
2. Determine which were committed (quorum acked) vs acked-only
3. Replay committed operations → complete writes
4. Remove WAL entries after replay
```

**Sequence numbers**: Monotonically increasing per-OSD. WAL entries include:
- `sequence_num: uint64`
- `operation: "PutMetadata" | "ReplicateMetadata" | "WriteData"`
- `shardKey: string` (for metadata ops)
- `ObjectMeta: bytes` (for metadata ops)
- `checksum: uint32` (CRC32C for data integrity)

---

## 4. Metadata Protocol (Distributed Replication)

### 4.1 Shard Ownership Resolution
```
1. Client → Any Node: GET /file.txt
2. Node: shardKey = consistentHash("file.txt", 256)
3. Node: shardOwner = ring.Lookup(shardKey) → Node A
4. Node: if local → BoltDB lookup; else RPC(Node A, "ResolveMetadata")
```

**RPC message format** (proposed from OpenSpec distributed-metadata-v1):
```
[shardOwner_xid: 8 bytes]
[opcode: 1 byte = 0x05 = ResolveMetadata]
[shardKey: variable, length-prefixed]
[reply_to_xid: 8 bytes]  // for correlation
```

### 4.2 Quorum Protocol for Metadata Writes
```
Write "foo.txt" → metadata replicas [A, B, G] (M=3)

1. Client → Node C: PUT /foo.txt
2. Node C: data placement via CRUSH → [N7, N42, N88] (data RF=3)
3. Node C: stream blob to data targets → quorum 2/3 ack (existing)
4. Node C: shardKey = consistentHash("foo.txt", 256)
5. Node C: shardOwner = ring.Lookup(shardKey) → Node A
6. Node C → Node A: RPC("PutMetadata", {name, hash, size, replicas})
7. Node A: write to local BoltDB
8. Node A → Node F: RPC("ReplicateMetadata", {shardKey, ObjectMeta})
9. Node A → Node G: RPC("ReplicateMetadata", {shardKey, ObjectMeta})
10. Node A: wait for quorum (2 of 3 acks) → success
11. Node C: return "200 OK" to client
```

**Quorum calculation**: `(metadata_replication/2)+1 = (3/2)+1 = 2`. So 2 of 3 replicas must acknowledge.

### 4.3 Read Protocol with Failover
```
Client → Node X: GET /foo.txt

1. Node X: check metadata cache (TTL=60s) → HIT? → serve from cache
2. Node X: shardKey = consistentHash("foo.txt", 256)
3. Node X: shardOwner = ring.Lookup(shardKey) → Node A
4. Node X: if X == Node A → meta = local BoltDB (<0.1ms)
   else: RPC(Node A, "ResolveMetadata", "foo.txt")
5. Node A: look up local BoltDB → {hash, size, replicas: [N7,N42,N88]}
6. Node A: return meta to Node X
7. Node X: cache metadata (TTL=60s)
8. Node X: check local blob → exist? → serve directly / proxy from best replica
```

**Failover if owner down**:
```
→ Fall back to metadata replicas: [Node 67, Node 91]
→ RPC(Node 67, "ResolveMetadata") → success
→ Node 67 returns same metadata (replica of same shard)
→ SWIM marks owner as SUSPECT
→ Scrub re-replicates to maintain 3 copies
```

---

## 5. Gossip & SWIM Protocol

### 5.1 Purpose
- **Failure detection**: Mark nodes as `SUSPECT` when they stop responding
- **Cluster membership**: disseminate membership changes across all daemons
- **Hinted handoff support**: Track which nodes are down/recovering
- **Scalability**: O(log N) gossip rounds for cluster-wide consistency

### 5.2 Message Types
```
SWIM gossip message format:
[gossip_round: uint32][gossip_type: uint8][sender_id][target_id]

gossip_type values:
  0 = ALIVE      — sender is healthy, reaching out
  1 = SUSPECT    — sender suspects target is failed
  2 = RECOVERY   — sender has recovered from suspect state
```

### 5.3 Gossip Algorithm
**Per gossip round** (every ~1 second):
1. Each node selects 3 random neighbors
2. Sends ALIVE gossip to those neighbors
3. Neighbors respond with their current view
4. Infection: If neighbor knows a node sender doesn't — sender learns it
5. If sender knows a node neighbor doesn't — neighbor learns it
6. After exchanging views, each node marks any unresponsive peer as `SUSPECT`

**Infection model**: Like a disease spreading through the cluster — eventually all nodes learn the same membership state.

### 5.4 Heartbeat Integration
- **OSD heartbeats**: `monc` collects OSD heartbeats every ~5s
- **MON heartbeats**: MON daemons send heartbeats to each other
- **Gossip supplements heartbeats**: SWIM provides *suspect* status before full failure detection
- **Timeouts**: 
  - Gossip round: ~1s
  - Suspect → timeout: configurable (default ~10-30s depending on subsystem)
  - MON quorum loss: 3-5 MONs, majority needed

### 5.5 What Gossip Does NOT Do
- **Not a consensus protocol**: SWIM is eventually consistent, not Paxos/Raft
- **Not for data replication**: Gossip propagates membership, not object data
- **Not for CRUSH map distribution**: CRUSH map comes from MON daemons, not gossip
- **Not for OSD-to-OSD replication**: Data replication uses primary+secondary via RPC, not gossip

### 5.6 Comparison: Ceph Gossip vs. Other Systems

| System | Gossip Protocol | Purpose |
|--------|----------------|---------|
| **Ceph** | SWIM | Failure detection + membership |
| **Cassandra** | Gossip | Membership + token ring maintenance |
| **Riak** | Gossip | Cluster state + partition info |
| **Kubernetes** | Endpoints/SD | Service endpoint tracking (not gossip) |
| **Docker Swarm** | Overlord | Central state (not gossip) |
| **Consul** | Serf | Member dispatch + event notification |

### 5.7 MomoFS Integration (from the design)
Since MomoFS is building on similar principles, the gossip/SWIM pattern would integrate as:

```
1. Each Momo node runs SWIM gossip (3 random neighbors per round)
2. ALIVE messages → maintain cluster membership view
3. SUSPECT messages → trigger hinted handoff replay
4. RECOVERY messages → invalidate cached metadata
5. SWIM state → inform GC loop (skip pinned blobs from suspect nodes)
6. Failure detection latency: O(log N) gossip rounds (~3-5s for small clusters)
```

---

## 6. S3/RGW Protocol (RGW Gateway)

### 6.1 REST API over TCP
```
HTTP Method + Path + Headers + Body
```

**Key endpoints**:
- `GET /bucket/object` → 307 redirect to OSD (if no swift emulator)
- `PUT /bucket/object` → stream to CRUSH-placed OSDs
- `DELETE /bucket/object` → tombstone + GC
- `HEAD /bucket/object` → metadata only
- `GET /bucket` → list objects (prefix/scanning)

### 6.2 Authentication
- **Swift-style**: `Authorization: AWS <key>:<signature>`
- **Signature**: HMAC-SHA256 of verb+path+headers+date+body
- **Temporary URLs**: For signed download/upload without long-lived keys

### 6.3 Response Codes
- `200 OK` / `201 Created`
- `202 Accepted` (async operations like large object upload)
- `301 Moved Permanently` (redirect to canonical hostname)
- `302 Found` (redirect for swift emulator compat)
- `403 Forbidden` (auth failure)
- `404 Not Found`
- `409 Conflict` (rename if target exists)
- `413 Payload Too Large` (upload > cluster size limit)
- `503 Service Unavailable` (cluster down, backoff recommended)

---

## 7. Comparison: Ceph vs. MomoFS Protocol Design

| Aspect | **Ceph** | **MomoFS (proposed)** |
|--------|----------|----------------------|
| **Transport** | BlueRing (same-process) + TCP (remote) | gRPC-style TCP + P2P gossip |
| **Message framing** | `[len:u32][op:u8][payload]` | `[xid:u64][op:u8][payload_len:u32][payload]` |
| **Correlation** | Connection IDs per session | `xid` per request for retry/redelivery |
| **Quorum protocol** | CRUSH-based primary+secondary | Consistent-hash shard + (M/2)+1 acks |
| **Failure detection** | SWIM gossip + heartbeats | SWIM gossip + explicit RPC timeout |
| **Recovery protocol** | WAL replay per OSD on restart | WAL + hinted handoff + shard re-replication |
| **Metadata distribution** | CRUSH → PG → OSD | Consistent hash ring → shard owner + replicas |
| **Configuration** | `ceph.conf` (monolithic) | TOML per-section (`[momofs]`, `[scrub]`, etc.) |
| **Idempotency** | Some ops are idempotent by design | Vector clocks + explicit retry tokens needed |

---

## 8. Key Protocol Design Principles (from Ceph 15+ years)

1. **Idempotent operations can retry**; non-idempotent need token-based dedup
2. **Timeouts should be configurable** — default 30s but tuned per workload
3. **Always include correlation IDs** (`xid`) for reply matching and retry logic
4. **Gossip is eventually consistent** — design state reconciliation accordingly
5. **WAL guarantees durability** — "acked" ≠ "committed"; need explicit commit sequence
6. **CRUSH removes central coordination** — placement is deterministic from inputs alone
7. **Separate failure domains** — OSD failure ≠ MON failure ≠ network partition
8. **Backpressure at the event-loop level** — Don't block the reactor; use async + futures

---

## 9. MomoFS Protocol Adaptations (from the three OpenSpec specs)

**Distributed-metadata-v1 protocol** adapts Ceph's quorum pattern:
- Instead of CRUSH-primary+secondary → consistent-hash shard owner + M replicas
- Quorum = `(M/2)+1` instead of Ceph's primary waits for N-1 secondaries
- Vector clocks replace Ceph's simpler primary-ack model for conflict resolution
- Cache TTL (60s) adapts Ceph's local-read-optimization pattern

**Inline-small-files-v1 protocol** adapts Ceph's BlueStore selective journaling:
- Only small blobs (≤4KB) go through "inline path"; large blobs bypass
- Same principle as BlueStore: only journal/write-through small writes
- Threshold configurable (4KB default) — like `min_alloc_size` (64KiB) but adjusted for object storage workloads

**Object-pinning-v1 protocol** has no direct Ceph equivalent but learns from:
- IPFS pinning (protect from GC)
- Ceph's `stoneway` / `deep-scrub` for bitrot detection
- BlueStore's WAL replay for crash recovery guarantees

---

## 10. Summary: Protocol Architecture

```
Application Layer         →  S3 API / POSIX FUSE / RBD
     ↓                              ↓
Protocol Layer              →  Custom TCP + RPC (xid-correlated)
     ↓                              ↓
Transport Layer             →  Seastar event-loop (Crimson) / BlueRing / TCP/IP
     ↓                              ↓
Consensus Layer             →  MON Paxos (cluster map) / SWIM (gossip)
     ↓                              ↓
Placement Layer             →  CRUSH deterministic algorithm
     ↓                              ↓
Device Layer                →  Raw block devices + BlueStore WAL / BoltDB KV
```

**Bottom line**: Ceph's protocol evolved from 15 years of production experience, trading some flexibility for battle-tested reliability. The key patterns (quorum-based replication, CRUSH placement, WAL crash recovery, SWIM gossip) are directly applicable to MomoFS, which is why the three OpenSpec specs (distributed metadata replication, inline small files, object pinning) reference these patterns explicitly.

---