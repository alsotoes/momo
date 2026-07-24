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
1. Selects up to `Fanout` (default: 3) random alive peers
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
gossip_port = 7946
heartbeat_interval = 1s
suspicion_timeout = 5s
fanout = 3
```

P2P is **disabled by default** and coexists with the existing `Communicator` interface.

## Testing

### Unit Tests (`src/p2p/*_test.go`)

- `types_test.go`: RPC encode/decode, heartbeat payload encode/decode, edge cases
- `peer_map_test.go`: Add/Remove/Get, RandomPeers, concurrent access
- `tcp_transport_test.go`: Listen/Dial/Send/Broadcast, connection lifecycle
- `gossip_test.go`: Heartbeat exchange, suspicion transitions, membership discovery
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

A peer in `SUSPECT` state is restored to `ALIVE` when any of the following arrives:
- `MsgHeartbeat` from the peer
- `MsgPing` from the peer
- `MsgAck` from the peer

This prevents false positives during transient network partitions.

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
- CAS garbage collection via decentralized refcounting

---

## Scatter-Gather Queries

### Overview

The `ScatterGather` struct enables distributed queries across the cluster. When a node receives a list request, it broadcasts a `MsgQuery` RPC to all alive peers, collects their responses within a timeout, and merges/deduplicates the results.

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

The `LeaseManager` provides time-bound, self-expiring leases for destructive operations (deletes). A lease must be granted by a majority quorum of alive peers before the operation proceeds. Leases are kept in-memory and expire automatically.

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgLeaseRequest` | 6 | Request a lease for a resource key |
| `MsgLeaseGrant` | 7 | Grant or deny a lease request |
| `MsgLeaseRelease` | 8 | Release a held lease |

### Protocol

1. **Acquire**: Node broadcasts `MsgLeaseRequest` to all alive peers
2. **Grant**: Each peer checks if the key is available (no active lease) and responds with `MsgLeaseGrant` (expiry > 0 = granted, expiry = 0 = denied)
3. **Quorum**: Acquirer needs majority (N/2 + 1) grants within timeout/2
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
```
