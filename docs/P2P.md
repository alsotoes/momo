# P2P Transport & Gossip Membership Protocol

## Overview

The `src/p2p` package implements a peer-to-peer transport layer with a gossip-based membership protocol for distributed cluster coordination. It enables Momo nodes to discover each other, track liveness, and disseminate membership information without relying on a central coordinator.

This module is the foundation for issue #248 (Gossip Membership, Scatter-Gather, Lease Consensus).

## Architecture

```
┌─────────────────────────────────────────────┐
│                  Gossiper                    │
│  ┌─────────────┐  ┌──────────────────┐      │
│  │ heartbeatLoop│  │  suspicionLoop   │      │
│  └──────┬──────┘  └────────┬─────────┘      │
│         │                    │                │
│  ┌──────┴────────────────────┴─────────┐    │
│  │          Transport (interface)       │    │
│  └──────────────────┬──────────────────┘    │
│                     │                        │
│  ┌──────────────────┴──────────────────┐    │
│  │          TCPTransport               │    │
│  │  ┌─────────┐  ┌──────────────────┐ │    │
│  │  │ acceptLoop│  │ handleConn/readLoop│ │    │
│  │  └─────────┘  └──────────────────┘ │    │
│  └─────────────────────────────────────┘    │
│                                              │
│  ┌─────────────────────────────────────┐    │
│  │            PeerMap                   │    │
│  │  peer1(alive) peer2(suspect) peer3  │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘
```

## Wire Format

All RPCs use a binary, length-prefixed frame format for zero-allocation encoding:

```
[4 bytes: total length] [1 byte: msg type] [4 bytes: from ID] [N bytes: payload]
```

**Safety limits:**
- `MaxPeersInHeartbeat = 256` — heartbeats are truncated to 256 peers to bound frame size.
- `maxPayloadSize = 1 MiB` — payloads exceeding 1 MiB are rejected to prevent memory exhaustion. Enforced in all `Decode*Payload` functions.

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgHeartbeat` | 1 | Periodic heartbeat with sender's peer list |
| `MsgMembership` | 2 | Node join/leave announcement |
| `MsgSuspect` | 3 | Suspicion announcement about a peer |
| `MsgPing` | 9 | Direct ping for failure detection |
| `MsgAck` | 10 | Ack response to a ping |
| `MsgIndirectPing` | 11 | Indirect ping request via intermediary |

### Heartbeat Payload

```
[4 bytes: peer count] [for each peer: 4 bytes ID + 2 bytes addr len + addr bytes]
```

Maximum peers per heartbeat: `MaxPeersInHeartbeat = 256` (prevents CPU exhaustion via malicious packets).

## Gossip Protocol

### Heartbeat Loop

Every `HeartbeatInterval` (default: 1s), the gossiper:
1. Selects up to `Fanout` random alive peers. When `fanout` is unset/`0`, it is
   **adaptive** (issue #825): `ceil(ln N)` for `N` alive peers, bounded to
   `[1, 10]`, so small clusters stay lean and large clusters converge faster. A
   positive `fanout` is a fixed override.
2. Encodes its current peer list as a `HeartbeatPayload`
3. Sends a `MsgHeartbeat` RPC to each selected peer

### Suspicion Loop

Every `HeartbeatInterval`, the gossiper checks all peers:
- **Alive → Suspect**: If `now - lastSeen > SuspicionTimeout` (default: 5s)
- **Suspect → Offline**: If `now - lastSeen > 2 * SuspicionTimeout`
- On transition to Offline, the `onLeave` callback is invoked

### RPC Handling

Received RPCs are processed by `HandleRPC`:
- `MsgHeartbeat`: Merge peer list into local PeerMap, invoke `onJoin` for new peers
- `MsgMembership`: Add announced peer to PeerMap, invoke `onJoin`
- `MsgSuspect`: Mark referenced peer as suspect if currently alive

## Panic Safety

All background goroutines (`heartbeatLoop`, `suspicionLoop`, `acceptLoop`, `handleConn`, `readLoop`, consumer loop) include `defer recover()` blocks that log the panic with a POSIX error constant (`syscall.EIO`). This follows Rule 37 (Unified Observable Panic Recovery).

## Configuration

Enable P2P in `momo.conf`:

```ini
[p2p]
enabled = true
gossip_port = 4450
gossip_interval = 1
suspicion_timeout = 5
fanout = 3
ping_timeout = 500
indirect_ping_count = 3
scatter_gather_timeout = 5
lease_timeout = 10
tls_cert_file = /etc/momo/p2p.crt
tls_key_file = /etc/momo/p2p.key
tls_ca_file = /etc/momo/p2p-ca.crt
```

P2P is **disabled by default** and coexists with the existing `Communicator` interface.

## TLS Encryption & Peer Authentication

The P2P transport supports **optional TLS encryption** and **mandatory peer ID authentication**.

### TLS (encryption)

Configure TLS under `[p2p]`:
- `tls_cert_file` — PEM certificate for this node
- `tls_key_file` — PEM private key for this node
- `tls_ca_file` — optional CA used to verify peer certificates

When `tls_cert_file` and `tls_key_file` are set, `TCPTransport` wraps its listener in `tls.NewListener` and dials via `tls.Dial`, negotiating TLS with a **minimum version of TLS 1.2**. When `tls_ca_file` is also set, **mutual TLS (mTLS)** is enforced via `ClientAuth = RequireAndVerifyClientCert`, where both the local certificate and each peer's certificate are validated against the CA pool (`RootCAs`/`ClientCAs`).

If TLS files are **not** configured, the node logs a `CRITICAL` warning that all P2P traffic is plaintext and falls back to plaintext operation — acceptable for dev/test only.

### Peer ID Authentication (AuthFunc)

Independent of TLS, every incoming connection is validated by an `AuthFunc`: `func(id int32) bool { return id >= 0 && int(id) < len(daemons) }`. The connecting peer's claimed ID must fall within the configured daemon set; otherwise the connection is rejected. This prevents peer-ID spoofing and injection of malicious gossip/membership messages. The server's `bootstrapP2P` passes both `TLSConfig` and `AuthFunc` into the `TCPTransportConfig`.

## Testing

### Unit Tests (`src/p2p/*_test.go`)

- `types_test.go`: RPC encode/decode, heartbeat payload encode/decode, edge cases
- `peer_map_test.go`: Add/Remove/Get, RandomPeers, concurrent access
- `tcp_transport_test.go`: Listen/Dial/Send/Broadcast, connection lifecycle, TLS (`TestTCPTransport_TLS`), peer auth (`TestTCPTransport_AuthFunc`)
- `gossip_test.go`: Heartbeat exchange, suspicion transitions, membership discovery
- `swim_test.go`: Ping/ack, indirect ping, adaptive RTT timeouts, RTT tracker EWMA, suspicion timeout
- `lease_test.go`: Lease acquire/release, no-peers edge case, lease expiry, quorum timeout
- `scatter_gather_test.go`: Scatter-gather query, large data handling, query payload encode/decode
- `integration_test.go`: 3-node cluster convergence, dynamic node join
- `benchmark_test.go`: RPC encode/decode, heartbeat encode/decode, peer map operations

Run with race detector:
```bash
go test -race -count=1 ./src/p2p/
```

### E2E Tests (`.github/scripts/test-e2e-p2p.sh`)

3-node process cluster verifying:
- Gossip convergence (all nodes discover each other)
- Failure detection (offline node marked suspect then offline)

```bash
make test-e2e-p2p
```

## SWIM Refinement

The gossip protocol extends the baseline heartbeat mechanism with SWIM-style failure detection: direct ping/ack, indirect pings, adaptive RTT-based timeouts, and suspicion restoration.

### Ping/Ack Protocol

Every `HeartbeatInterval`, the gossiper sends a `MsgPing` to one random alive peer. The peer responds with `MsgAck`. If the ack arrives within `PingTimeout`, the RTT is recorded. If no ack arrives, an indirect ping is initiated.

### Indirect Ping

When a direct ping to peer *T* times out, the gossiper selects up to `IndirectPingCount` random peers and sends each a `MsgIndirectPing` targeting *T*. Each intermediary forwards the ping to *T* and, if it receives an ack, forwards the ack back to the original requester. If no indirect ack is received, *T* is marked `SUSPECT`.

### RTT Tracking & Adaptive Timeouts

Per-peer RTT is tracked using an exponentially weighted moving average (EWMA, alpha=0.25). The suspicion timeout is adapted:

```
adaptive_timeout = max(SuspicionTimeout, min(RTT * 10, 5 * SuspicionTimeout))
```

- Falls back to `SuspicionTimeout` when no RTT data is available
- Scales with RTT for slower peers, capped at 5x the base timeout

### Suspicion Restoration

A peer in `SUSPECT` or `OFFLINE` state is restored to `ALIVE` when any of the following arrives:
- `MsgHeartbeat` from the peer
- `MsgPing` from the peer
- `MsgAck` from the peer

This prevents false positives during transient network partitions and enables recovery of formerly offline nodes without operator intervention.

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgPing` | 9 | Direct ping to a peer |
| `MsgAck` | 10 | Ack response to a ping |
| `MsgIndirectPing` | 11 | Request to ping a target on behalf of another node |

### Ping Payload

```
[8 bytes: ping ID] [4 bytes: target ID] [8 bytes: timestamp unixnano]
```

### Configuration

```ini
[p2p]
ping_timeout = 500          # milliseconds
indirect_ping_count = 3     # peers to ask for indirect ping
```

## Future Work

- Compression for large heartbeat payloads

---

## Scatter-Gather Queries

### Overview

The `ScatterGather` struct enables distributed queries across the cluster. When a node receives a list request, it broadcasts a `MsgQuery` RPC to alive peers, collects their responses within a timeout, and merges/deduplicates the results. Peer selection uses **quality-aware ordering** (issue #823): alive peers are ranked by EWMA RTT (`AliveByQuality`), so low-latency peers are contacted first and suspect/offline peers are excluded.

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgQuery` | 4 | Scatter-gather query request |
| `MsgQueryResponse` | 5 | Scatter-gather query response |

### Query Types

| Query | Description |
|-------|-------------|
| `QueryList` | List all local files |
| `QueryGet` | Get metadata for a specific file |
| `QueryHas` | Check if a hash exists locally |
| `QueryDelete` | Delete a file by hash (requires lease consensus) |

### RPC Routing

Query RPCs are multiplexed on the existing `transport.Consume()` channel alongside gossip heartbeats. The `Gossiper.HandleRPC` routes `MsgQuery` and `MsgQueryResponse` to the `ScatterGather.HandleRPC` method.

### Server Integration

- `StorageQueryHandler` (in `src/server/query_handler.go`) implements `p2p.QueryHandler` over the local CAS store
- `ScatterGatherLister` adapts `ScatterGather` to the `transport.GlobalLister` interface
- When P2P is enabled, S3 `ListObjectsV2` and native list operations use scatter-gather to return a unified global directory
- Results are merged and deduplicated by content hash

### Configuration

```ini
[p2p]
scatter_gather_timeout = 5  # seconds
```

---

## Lease-Based Consensus

### Overview

The `LeaseManager` provides time-bound, self-expiring leases for destructive operations (deletes). A lease must be granted by a majority quorum of alive peers before the operation proceeds. Leases are kept in-memory and expire automatically. Quorum membership is selected with **quality-aware ordering** (issue #823): alive peers are ranked by EWMA RTT so low-latency peers are preferred, and suspect/offline peers are excluded.

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgLeaseRequest` | 6 | Request a lease for a resource key |
| `MsgLeaseGrant` | 7 | Grant or deny a lease request |
| `MsgLeaseRelease` | 8 | Release a held lease |

### Protocol

1. **Acquire**: Node broadcasts `MsgLeaseRequest` to all alive peers
2. **Grant**: Each peer checks if the key is available (no active lease) and responds with `MsgLeaseGrant` (expiry > 0 = granted, expiry = 0 = denied)
3. **Quorum**: Acquirer needs `peerCount/2 + 1` grants from peers within timeout/2, where `peerCount` excludes self. For small clusters this effectively requires unanimous peer agreement.
4. **Release**: After operation completes, broadcasts `MsgLeaseRelease`
5. **Expiry**: Background loop cleans up expired leases every 500ms

### Server Integration

- `LeaseAcquirerAdapter` adapts `LeaseManager` to the `transport.LeaseAcquirer` interface
- When P2P is enabled, S3 `DELETE` and native delete operations acquire a lease before proceeding
- If lease acquisition fails (quorum not reached), returns 503 Service Unavailable (S3) or error status (native)

### Configuration

```ini
[p2p]
lease_timeout = 10  # seconds
```

## CAS Garbage Collection

The `src/storage` package implements reference-counted garbage collection for content-addressable blobs, with P2P delete propagation via scatter-gather.

### Reference Counting

- Each blob in the `objects` bucket stores an `ObjectMeta` struct: `{Size, RefCount, DeletedAt}`
- `Put` increments `RefCount` when the blob already exists (deduplication)
- `Delete` decrements `RefCount` and writes a tombstone
- When `RefCount` reaches 0, the blob is eligible for GC

### Tombstones

- A `tombstones` bucket maps `FileName -> deletion timestamp` (unix nano)
- Tombstoned entries are hidden from `List`, `Get`, and `GetBlobPath`
- Tombstones support **resurrection**: re-`Put` of a tombstoned name clears the tombstone
- `GetTombstones()` and `ApplyTombstone()` enable P2P tombstone exchange for eventual consistency

### GC Sweeper

- Background goroutine runs every `gc_interval` seconds (default 300 = 5 minutes)
- **Sweep orphaned blobs**: removes on-disk blob files and `objects` entries with `RefCount=0`
- **Sweep expired tombstones**: removes tombstones older than `tombstone_retention` (default 86400 = 24 hours)

### P2P Delete Propagation

- `QueryDelete` (type 4) is sent via scatter-gather to all peers when a delete occurs
- `ScatterGatherDeleter` adapts `ScatterGather` to the `transport.DeletePropagator` interface
- S3 `DELETE` and native delete handlers fan out deletes to all peers (best-effort)
- Each peer applies the delete locally, writing its own tombstone

### Backward Compatibility

- Legacy `objects` bucket entries (ASCII size only) are automatically migrated on read
- `decodeObjectMeta` detects the 24-byte binary format vs. legacy ASCII and falls back gracefully

### Configuration

```ini
[storage]
gc_interval = 300          # seconds (5 minutes)
tombstone_retention = 86400 # seconds (24 hours)
```
