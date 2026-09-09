# 0053-protocol-cluster-separation

## Status
Accepted

## Confidence
High

## Context
Momo supports three wire protocols (native TCP, native QUIC, S3-compatible REST) that all need to interoperate with the same cluster logic: CRUSH placement, replication strategies (Chain/Splay/PrimarySplay), P2P membership/gossip, lease consensus, scatter-gather queries, R2 self-heal rebuild, R6 metadata HA, and the storage CAS engine.

Historically, the project adopted interface-based boundaries as an implementation idiom. The critical boundaries are:

- `transport.Communicator` — the wire protocol abstraction (handshake, metadata exchange, payload transfer)
- `p2p.Transport` — the P2P cluster transport (gossip, leases, scatter-gather, OPRF)
- `storage.Store` — the storage abstraction (CAS deduplication, blob storage, metadata)

However, this separation was never formally codified as an architectural decision. The risk was that new features or protocols might accidentally leak protocol-specific types into cluster logic, coupling the cluster to specific transports and breaking the polymorphic promise (single port, multi-protocol, protocol-agnostic replication).

## Decision

**Codify three interface contracts as the permanent separation boundaries between protocols and cluster logic:**

| Interface | Package | Implemented By | Consumed By |
|---|---|---|---|
| `transport.Communicator` | `transport` | `MomoTCPCommunicator`, `MomoQUICCommunicator`, `S3Communicator` | `server.Daemon`, `client.Connect` |
| `p2p.Transport` | `p2p` | `TCPTransport` | `server.bootstrapP2P`, `p2p.ScatterGather`, `p2p.LeaseManager`, `p2p.OPRFProvider` |
| `storage.Store` | `storage` | `CASStore` | `server.Daemon`, `DaemonRebuildSource`, `R6 metadata RPC` |

**Enforcement Rules:**

1. **Cluster logic must not import protocol implementations.** The following packages must never be imported by `server`, `common`, `p2p`, `storage`, `R2`, `R6`, `R3`, `R4`:
   - `github.com/alsotoes/momo/src/transport/momo_tcp`
   - `github.com/alsotoes/momo/src/transport/momo_quic`
   - `github.com/alsotoes/momo/src/transport/s3_communicator`

2. **Cluster logic must not type-assert `Communicator` to concrete protocol types.** The only allowed interface extensions are the optional capability interfaces defined in `transport/communicator.go`: `GlobalLister`, `LeaseAcquirer`, `DeletePropagator`, `MetricsHook`, `LatencyRecorder`, `OPRFService`, `ChecksumProvider`. These are injected via `Set*` methods by the factory.

3. **The `ProtocolFactory` (`transport/factory.go`) is the single protocol-aware component.** It dispatches based on `[global] protocol` config and returns the `Communicator` interface.

4. **New wire protocols must implement `transport.Communicator`** and register in the factory. No other cluster code changes required.

5. **Interface compliance tests** in `src/transport/`, `src/p2p/`, `src/storage/`, and `src/server/` verify the boundaries at CI time.

## Consequences

**Positive:**
- Adding a new wire protocol (e.g., gRPC, WebSocket, HTTP/3) only requires implementing `Communicator` and registering it in the factory — zero changes to replication, P2P, storage, or R2-R6 logic.
- Protocol-specific bugs cannot leak into cluster logic (no shared state, no type coupling).
- Each layer is independently testable with mock implementations.
- The polymorphic promise (single port, multi-protocol, protocol-agnostic replication) is architecturally guaranteed.

**Negative:**
- Slight indirection cost (interface call vs direct call) — negligible (~1-2ns) and dwarfed by I/O.
- Optional capabilities require `Set*` injection pattern — adds boilerplate for new capabilities.
- New developers must understand the interface contract before modifying cluster logic.

## Alternatives Considered

1. **Direct protocol coupling** — cluster logic imports and calls protocol types directly.
   - Rejected: breaks polymorphism, makes new protocols require invasive changes, couples testing.

2. **Plugin-based dynamic loading** (`buildmode=plugin`, go-plugin).
   - Rejected: RPC/serialization overhead on hot path, code audit surface explosion, conflicts with verify-on-read integrity model.

3. **Single monolithic protocol** (drop S3, keep only native).
   - Rejected: S3 gateway is a core product feature for cloud-native tooling compatibility.

## Implementation Status
- **Code**: Done — all three interfaces exist and are used throughout the codebase
- **Tests**: Added — compliance tests in `src/transport/`, `src/p2p/`, `src/storage/`, `src/server/`
- **Docs**: Updated — `ARCHITECTURE.md` now documents the contract explicitly

## References
- Issue: #131 (S3 gateway), #133 (S3 over QUIC), #225 (lightweight S3 design)
- ADRs: 0014 (add-quic-protocol), 0016 (add-s3-protocol), 0031 (plugin-seam-architecture), 0033-0035 (R1-R3)
- Blog: docs/blog/posts/024-bolt-performance-engineering.md, docs/blog/posts/044-plugin-seam-architecture.md
- Spec: `openspec/changes/plugin-seam-architecture/`
