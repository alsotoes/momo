# Momo

Momo is a high-performance, transport-agnostic file replication playground written in Go. It demonstrates several replication strategies and a simple, metrics‑driven controller that can switch strategies at runtime (a “polymorphic” system), optimized with zero-allocation techniques. It fully supports both legacy TCP (`momo-tcp`) and modern QUIC (`momo-quic`) transports.

This document explains the architecture, configuration, wire protocol, replication modes, and how to run the client and servers.

## Key Performance & Security Features (⚡ Bolt & 🛡️ Sentinel)

- **Balanced Primary Architecture**: Removes central bottlenecks by deterministically selecting primary nodes for each object using the CRUSH-lite algorithm.
- **Automated AI Governance**: Pull Requests are automatically reviewed and merged by a Gemini-powered audit engine that enforces strict architectural steering rules.
- **Content-Addressable Storage (CAS)**: Saves disk space and bandwidth by identifying files by their SHA-256 content hash, with built-in server-side deduplication.
- **Pluggable Transport Layer**: Communicate seamlessly over raw TCP, encrypted QUIC (TLS 1.3), or S3-compatible REST gateways via a modular `ProtocolFactory`.
- **Zero-Allocation Hashing & Encoding**: SHA-256 sums and hex encoding use stack-allocated buffers to eliminate heap escapes.
- **Phased Absolute Deadlines**: Continuous protection against Slowloris attacks with strict bounds for handshake (10s), metadata (60s), and dynamic transfer phases.
- **Bitwise Deadline Amortization**: Reduces `SetDeadline` system calls by ~98% in hot paths.
- **Consolidated Network I/O**: Merges authentication tokens, timestamps, and payloads into unified writes to minimize syscalls and Nagle delays.
- **Security Hardening**: Mandatory 64-byte AuthToken validation, CRLF log injection protection, and comprehensive `AUDIT:` logging for all sensitive operations.
- **P2P Cluster Coordination**: Gossip-based membership with SWIM-style failure detection (direct ping/ack, indirect ping, adaptive RTT timeouts), scatter-gather queries, and lease-based consensus for deletes.

## Repository Layout

- `.github/scripts/`: Automation and governance scripts.
  - `ai_reviewer.py`: Python-based Gemini AI code review engine.
  - `test-e2e.sh`: End-to-end integration test runner.
  - `update_readme_with_benchmarks.sh`: Automated documentation updater.
- `src/momo.go`: Entry point (client/server runner and metrics bootstrap).
- `src/transport/`: Pluggable communication layers and protocol implementations.
  - `communicator.go`: Central `Communicator` and `MomoListener` interfaces.
  - `factory.go`: `ProtocolFactory` for instantiating transports.
  - `momo_tcp.go`: Legacy TCP implementation.
  - `momo_quic.go`: Modern QUIC implementation using `quic-go`.
  - `s3_communicator.go`: S3-compatible REST API mapping.
- `src/client/`: Client-side logic for cluster replication and file forwarding.
  - `client.go`: Main cluster connection and parallel file transmission logic.
- `src/common/`: Agnostic, shared utilities.
  - `config.go`: Optimized INI configuration loader.
  - `hash.go`: Optimized file SHA-256 hashing.
  - `log.go`: Secure logging with CRLF sanitization.
  - `string.go`: Performance-tuned string padding.
  - `constants.go`: Shared system-wide protocol constants.
- `src/server/`: Server daemon and file reception logic.
  - `server.go`: Core Daemon loop utilizing pluggable transports.
  - `file.go`: Secure metadata parsing and file writing.
  - `replication.go`: Dynamic replication mode control server.
- `src/storage/`: Content-Addressable Storage (CAS) engine.
  - `storage.go`: Bbolt-backed object store with tiered directory layout.
- `src/p2p/`: P2P transport layer with gossip membership protocol.
  - `types.go`: Peer, RPC, HeartbeatPayload, PingPayload with binary length-prefixed encoding.
  - `transport.go`: Transport interface (Listen, Dial, Consume, Broadcast, Send).
  - `tcp_transport.go`: TCPTransport implementation with connection tracking.
  - `peer_map.go`: Thread-safe PeerMap with RandomPeers for gossip fanout.
  - `gossip.go`: Gossiper with heartbeat, SWIM ping/ack, indirect ping, adaptive RTT timeouts, suspicion.
- `src/metrics/`: Performance monitoring and polymorphic control loop.
- `conf/momo.conf`: Secure configuration example.

## Replication Modes & Handshake Actions

Handshake Requested Mode constants (see `src/common/constants.go`):

### 📈 Replication Strategies (Numeric codes)
- `0`: **No Replication**: Standalone storage on the selected primary node.
- `1`: **Chain Replication**: Pipelined chain replication. Client uploads to Primary, which chain-forwards sequentially down the cluster.
- `2`: **Splay Replication**: Server-side splaying. Client uploads a single copy, and the Primary splays it in parallel to all other nodes.
- `3`: **Primary-Splay Replication (Client-Splay)**: Client-side splaying. Shifts replication workload to the client, which copies the payload concurrently to all replica nodes in parallel.

### 🔌 Native Query Actions (ASCII character codes)
- `'L'`: **ModeList**: Query directory list of all stored file objects.
- `'D'`: **ModeDelete**: Request specific file deletion mapping on BoltDB.
- `'G'`: **ModeGet**: Request native file payload retrieval (Download).

## Data Flow

Handshake and transfer overview:

1. **Secure Handshake**: Client opens a network connection (TCP, QUIC, or S3) and sends a combined **84-byte packet** (64-byte AuthToken + 19-byte Timestamp + 1-byte RequestedMode). The `RequestedMode` byte is polymorphic: numbers (`'0'`-`'9'`) represent replication modes, while characters (`'L'`, `'D'`, `'G'`) represent non-replication query actions.
2. **Replication Mode Confirmation**: Server responds with a **1-byte confirmation** of the final replication strategy.
3. **Decoupled Metadata Check**: Client sends the **192-byte file metadata packet** (64-byte Hash + 64-byte Name + 64-byte Size). Server replies with a 1-byte code indicating if the transfer is required (`'1'`) or can be skipped (`'2'`) due to matching hash database existence (**CAS Deduplication**).
4. **Streamed Payload**: Client streams file bytes until EOF.
5. **Validation & ACK**: Server writes to disk via `io.TeeReader` (simultaneous hashing), validates integrity, and replies with `ACK{serverId}`.


## Verification & Testing

Momo has a highly mature local verification framework composed of unit, integration, and end-to-end (E2E) testing targets:

1.  **Transport Decoupling Tests (`factory_test.go`)**: Verifies pluggable dialers and listeners.
2.  **Concurrency Leak Checks (`goleak`)**: Enforces absolute thread/connection leak hygiene on all transports.
3.  **End-to-End TCP Replication (`smoke-tcp`)**: Verifies data distribution across 3 virtual TCP daemons.
4.  **End-to-End QUIC Replication (`smoke-quic`)**: Verifies secure data replication over encrypted QUIC streams.
5.  **Scale & CAS Engine (`smoke-scale-cas`)**: A high-integrity stress test simulating a **5-node cluster** with a **replication factor of 3**. It explicitly verifies:
    *   **CRUSH-lite Placement**: Deterministic data distribution across heterogeneous nodes.
    *   **Content-Aware Deduplication**: Server-side "Deduplication hits" that skip redundant uploads.
    *   **Bbolt Persistence**: Transactional metadata integrity across multiple virtual daemons.

### Running Tests Locally

```bash
# Run all unit and integration tests
make test

# Run a specific smoke test
make smoke-scale-cas
```

Momo includes a built-in benchmarking suite and performance history tracking. Refer to the [Performance Guide](PERFORMANCE.md) for the latest metrics.
