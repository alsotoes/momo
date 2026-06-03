# Change: QUIC Protocol for Global Cluster Replication
**Related Issue:** #132

## Why
Momo currently relies on standard TCP for all file transfers. While efficient for local networks, TCP suffers from Head-of-Line (HoL) blocking and high latency under packet loss, especially in geographically distributed clusters or over unstable WAN links. Implementing the QUIC protocol (UDP-based multiplexing) across **all replication modes** (None, Chain, Splay, PrimarySplay) will drastically improve throughput, security, and stability for both edge clients and internal cluster communication.

### Architectural Rationale: QUIC as the Standard Transport
Using QUIC (Quick UDP Internet Connections) globally provides a modern, secure-by-default foundation for Momo:

#### Advantages for All Replication Tiers
- **Eliminates Head-of-Line (HOL) Blocking:** QUIC supports multiple independent streams. A lost packet in one replication path (e.g., node 0 to node 1) does not stall other concurrent transfers.
- **Superior Performance on Unstable Networks:** QUIC's resilience to packet loss ensures high throughput even on suboptimal network links between data centers or edge nodes.
- **Mandatory Encryption:** QUIC requires TLS 1.3, providing end-to-end encryption for all file data and metadata by default, removing the risk of plain-text TCP sniffing.
- **Connection Migration:** Internal replication can recover seamlessly from transient network changes or IP rotations without breaking long-running file transfers.
- **Faster Handshakes:** 0-RTT/1-RTT handshakes reduce the latency of initiating replication steps.

#### Coexistence and Transition
- **Dual-Stack Support:** Daemons will listen on both TCP and UDP/QUIC ports to maintain backward compatibility and allow for a gradual transition.
- **Protocol Configuration:** A new `protocol` field in the `[global]` section of `momo.conf` will explicitly control the preferred transport (`protocol=tcp` or `protocol=quic`). This ensures all replication modes behave identically from a logic perspective while utilizing the chosen network stack.
- **Polymorphic Transport:** The system will dynamically choose the best transport based on this configuration, providing a fallback to TCP for high-speed local LANs where kernel-level optimization is required.

## What Changes
- Integrate `github.com/quic-go/quic-go` into the Momo technology stack.
- Implement a **Transport-Agnostic Layer** in `src/common` to unify `net.Conn` and `quic.Stream` operations.
- Upgrade UDP/QUIC listeners to run globally on all server nodes, handling all replication mode logic.
- Refactor `Connect`, `SendFile`, and `getFile` to support QUIC multiplexing across all modes.
- Ensure dynamic hot-swapping remains fully functional: connections of either protocol must finish gracefully during configuration shifts.

## Impact
- Affected specs: `replication`, `security`
- Affected code: `src/common` (dialer/listener abstractions), `src/server` (Daemon logic), `go.mod`


