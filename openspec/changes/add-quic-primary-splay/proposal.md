# Change: QUIC Protocol for Primary Splay Replication

## Why
Momo's `PrimarySplay` replication mode dictates that a single client directly uploads payload chunks to all nodes in the cluster simultaneously over a WAN. Standard TCP connections suffer from Head-of-Line (HoL) blocking and high latency under packet loss over WANs, rendering `PrimarySplay` unstable and slow. Implementing the QUIC protocol (UDP-based multiplexing) exclusively for the `PrimarySplay` replication tier will drastically improve throughput and stability for edge clients, while preserving Momo's elegant dynamic hot-swapping behavior (allowing older TCP streams to finish cleanly on other tiers).

## What Changes
- Integrate `github.com/quic-go/quic-go` into the Momo technology stack.
- Implement UDP/QUIC listeners running concurrently alongside the standard TCP daemon.
- Refactor the `ReplicationPrimarySplay` client handshake and streaming logic to utilize QUIC streams instead of `net.Conn`.
- Ensure dynamic hot-swapping remains fully functional: switching into `PrimarySplay` spawns new QUIC client dials, while switching out spawns TCP dials. Pre-existing connections of either protocol must finish gracefully without restarting the daemon.

## Impact
- Affected specs: `replication`
- Affected code: `src/common` (dialer/listener interfaces), `src/server` (Daemon logic), `go.mod`
