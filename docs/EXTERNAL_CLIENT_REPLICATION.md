# External Client Replication Mode Handling

This document describes how Momo handles replication mode selection when external S3 clients (e.g., aws-cli, boto3, s3cmd) connect to a server whose polymorphic replication mode requires client-side participation.

## Background

Momo supports four replication modes:

| Mode | ID | Side | Description |
|---|---|---|---|
| `ReplicationNone` | 0 | Server | No replication; store on receiving node only |
| `ReplicationChain` | 1 | Server | Node 0 → Node 1 → Node 2 (sequential) |
| `ReplicationSplay` | 2 | Server | Node 0 → Node 1 and Node 2 (concurrent fan-out) |
| `ReplicationPrimarySplay` | 3 | Client | Client → Node 0, Node 1, Node 2 (concurrent, client-driven) |

**Server-side modes** (1, 2): The server receives the file and handles replication to other nodes. Works with any client.

**Client-side modes** (3): The server just stores the file locally. The *client* is responsible for computing CRUSH placement and uploading to all replica nodes in parallel. Requires a momo-aware client.

## The Problem

When an external S3 client like aws-cli connects, it sends standard S3 headers (`Authorization: AWS4-HMAC-SHA256`, `X-Amz-Date`) but does **not** send momo-specific headers (`X-Momo-Requested-Mode`, `X-Momo-Timestamp`).

### Bug 1: Peer Misidentification

The S3 communicator falls back to `X-Amz-Date` for the timestamp, which parses to a valid UnixNano value — **not** `DummyEpoch`. The server's mode negotiation logic (`server.go:208-221`) treats this as a forwarded peer connection and trusts `requestedMode = 0` (`ReplicationNone`). The file is stored with **no replication**, regardless of the server's configured mode.

### Bug 2: Client-Side Mode Incompatibility

Even if the server used its configured mode, `primary-splay` (mode 3) requires the *client* to fan out to replicas. The server handles mode 3 identically to `ReplicationNone` — no server-side replication. An external S3 client cannot perform client-side replication because it doesn't know the momo protocol.

## The Solution: Config-Driven Set Subtraction

A new config variable `client_side_replication_modes` declares which mode IDs require a momo-aware client. The effective modes for external clients are computed at runtime as a **set subtraction**:

```
effective_modes = replication_order \ client_side_replication_modes
```

### Configuration

```ini
[global]
replication_order=3,2,1
client_side_replication_modes=3
```

- `replication_order`: The full polymorphic switching order (used by momo CLI clients).
- `client_side_replication_modes`: Comma-separated list of mode IDs that require a momo-aware client. Default: `3`.

### Effective Modes by Client Type

| Client | Effective Modes | Example with `replication_order=3,2,1` |
|---|---|---|
| momo CLI | `replication_order` (full list) | `{3, 2, 1}` |
| External S3 (aws-cli) | `replication_order \ client_side_replication_modes` | `{2, 1}` |

### Runtime Behavior

1. **External client detection** (`s3_communicator.go`): If `X-Momo-Requested-Mode` header is absent, the connection is flagged as external and `timestamp` is forced to `DummyEpoch` so the server uses its configured replication mode.

2. **Per-transaction mode downgrade** (`server.go`): After selecting the server's current polymorphic mode, if the connection is external AND the selected mode is in `client_side_replication_modes`, the server walks `replication_order` forward to find the next mode NOT in that list. That mode is used for this transaction only.

3. **Global state unchanged**: The polymorphic system continues cycling through the full `replication_order` based on load. The per-connection downgrade does not mutate global state.

### Example Scenario

```
T0: Global state = 3 (primary-splay)
    momo CLI client connects → uses mode 3 → client does fan-out

T1: Global state = 3 (unchanged)
    aws-cli client connects → detected as external
    → mode 3 is in client_side_replication_modes
    → downgrade to mode 2 (splay) for this transaction only
    → server does fan-out replication
    → global state stays at 3

T2: Global state = 3 (unchanged)
    momo CLI client connects → uses mode 3 → client does fan-out
```

## Key Properties

- **No hardcoded mode numbers in code.** The filtering is purely config-driven.
- **Already-started connections cannot be mutated.** The replication mode is locked at handshake time. New connections get the current global state (filtered if external).
- **Extensible without code changes.** Future client-side or mixed modes just need to be added to `client_side_replication_modes` in config.

### Adding a Future Mixed Mode

If a new mode 4 (e.g., mixed client+server replication) is added and it requires a momo-aware client:

```ini
replication_order=4,3,2,1
client_side_replication_modes=3,4
```

- momo CLI: `{4, 3, 2, 1}` (full list)
- aws-cli: `{2, 1}` (modes 3 and 4 subtracted)

No code changes required — just update the config.

## Interaction with the Polymorphic System

The polymorphic system (`src/metrics/metrics.go`) runs on node 0 and cycles through `replication_order` based on CPU/memory load, broadcasting changes cluster-wide. This sets the global replication state (`repState.New`).

| Event | Global State | momo CLI client | aws-cli client |
|---|---|---|---|
| Load low → mode 3 | 3 | Uses 3 (primary-splay) | Downgraded to 2 (splay) |
| Load high → mode 1 | 1 | Uses 1 (chain) | Uses 1 (chain) — no downgrade needed |
| Load medium → mode 2 | 2 | Uses 2 (splay) | Uses 2 (splay) — no downgrade needed |

The polymorphic system never knows or cares that an external client connected. It keeps broadcasting the full order. The filtering happens at the connection handler level, after the global mode is already selected.

## Related

- [Issue #258](https://github.com/alsotoes/momo/issues/258) — Original question about aws-cli replication behavior
- [Polymorphic System](POLYMORPHIC_SYSTEM.md) — Dynamic replication mode switching
- [Wire Protocol](PROTOCOL.md) — Handshake and mode negotiation details
- [S3 Protocol](PROTOCOL.md#s3-protocol) — S3 gateway and REST handler
