# Change: QUIC Protocol for Primary Splay Replication

## Why
Momo's `PrimarySplay` replication mode dictates that a single client directly uploads payload chunks to all nodes in the cluster simultaneously over a WAN. Standard TCP connections suffer from Head-of-Line (HoL) blocking and high latency under packet loss over WANs, rendering `PrimarySplay` unstable and slow. Implementing the QUIC protocol (UDP-based multiplexing) exclusively for the `PrimarySplay` replication tier will drastically improve throughput and stability for edge clients, while preserving Momo's elegant dynamic hot-swapping behavior (allowing older TCP streams to finish cleanly on other tiers).

### Architectural Rationale: QUIC vs TCP
Using QUIC (Quick UDP Internet Connections) instead of TCP for a filesystem is an excellent architectural decision for specific, modern, or geographically distributed scenarios (like remote access or unreliable mobile/Wi-Fi networks). Here is a breakdown of why Momo adopts QUIC for `PrimarySplay` while retaining TCP for internal cluster replication (`Chain`, `Splay`):

#### Why QUIC is Good for Remote Filesystems (PrimarySplay / WAN)
- **Eliminates Head-of-Line (HOL) Blocking:** In TCP, if one packet is lost, all subsequent data must wait for it. QUIC supports multiple independent streams, so a lost packet in one file transfer does not stall other transfers.
- **Superior Performance on Lossy Networks:** QUIC is more resilient to packet loss than TCP, maintaining high throughput in environments with 10% packet loss, whereas TCP performance drops significantly.
- **Connection Migration (Mobile/Unstable Networks):** If a client switches networks (e.g., Wi-Fi to cellular), TCP breaks. QUIC uses a unique Connection ID, allowing the session to continue seamlessly.
- **Faster Setup (0-RTT):** QUIC combines transport and encryption handshakes, allowing faster reconnection, which is ideal for "chattier" file protocols.
- **Built-in Encryption:** QUIC forces encryption (TLS 1.3), providing better security by default over the WAN.

#### When to Stick with TCP (Chain, Splay / LAN)
- **High-Speed Local Networks (LAN):** In a stable, high-bandwidth LAN (e.g., 10Gbps+ inside the cluster), the user-space processing of QUIC can be slower than kernel-level TCP.
- **Lower CPU Usage:** QUIC is generally more computationally intensive due to user-space processing of packets, which can be a bottleneck on resource-constrained devices or tightly-coupled internal replication.

## What Changes
- Integrate `github.com/quic-go/quic-go` into the Momo technology stack.
- Implement UDP/QUIC listeners running concurrently alongside the standard TCP daemon.
- Refactor the `ReplicationPrimarySplay` client handshake and streaming logic to utilize QUIC streams instead of `net.Conn`.
- Ensure dynamic hot-swapping remains fully functional: switching into `PrimarySplay` spawns new QUIC client dials, while switching out spawns TCP dials. Pre-existing connections of either protocol must finish gracefully without restarting the daemon.

## Impact
- Affected specs: `replication`
- Affected code: `src/common` (dialer/listener interfaces), `src/server` (Daemon logic), `go.mod`

