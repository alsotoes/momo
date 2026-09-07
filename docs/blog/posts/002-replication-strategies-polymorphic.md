---
title: "Replication Strategies and the Polymorphic Engine"
date: 2026-05-04T18:54:37Z
draft: false
post_type: architecture
tags: [go, replication, architecture, bolt, sentinel]
categories: [origin]
summary: "Chain, Splay, and Primary-Splay replication — and the metrics-driven controller that switches between them at runtime."
artifacts:
  - {type: doc, path: docs/REPLICATION_STRATEGIES.md}
  - {type: spec, path: openspec/changes/add-replication-durability-floor}
related:
  - 001-origin-and-genesis
  - 003-transport-tcp-to-quic
  - 021-r3-write-durability-quorum
---

Most distributed storage architectures lock their replication topology into their core design: Ceph uses primary-OSD peering, Cassandra uses client-side coordinator fan-out, and HDFS uses a pipelined daisy-chain. Each topology optimizes for a specific constraint at the expense of others.

momo was designed around an alternative premise: **why hardcode a single replication topology when network conditions, hardware heterogeneity, and client types shift over time?**

The **Polymorphic Engine** allows a running momo cluster to dynamically shift its active replication strategy without restarting daemons or dropping in-flight connections.

{{< diagram src="/diagrams/02-replication-strategies.svg" alt="Replication strategies overview" caption="Figure 1: High-level overview of momo's four replication strategies" >}}

---

## 1. The Core Topologies: Splay vs. Chain

In `src/common/struct.go`, momo formalizes four distinct replication modes:

```go
const (
    ReplicationNone         = 0 // Local storage only (no network replication)
    ReplicationSplay        = 1 // Primary server fans out concurrently to replicas
    ReplicationChain        = 2 // Pipelined daisy-chain (N0 -> N1 -> N2)
    ReplicationPrimarySplay = 3 // Client streams to all replicas concurrently
)
```

{{< diagram src="/diagrams/02a-chain-splay.svg" alt="Splay vs Chain replication" caption="Figure 2: Concurrent server fan-out (Splay) vs sequential pipelining (Chain)" >}}

### Splay Replication (Mode 1)
The client sends one stream to the primary node ($N_0$). $N_0$ writes the payload to its local CAS store while simultaneously fanning out concurrent TCP/QUIC streams to secondary replicas ($N_1, N_2$).
- **Latency**: $O(1)$ network hops—fastest write latency.
- **Cost**: The primary node consumes $(R-1) \times \text{Size}$ of network egress bandwidth, creating a bottleneck on the primary server's network card.

### Chain Replication (Mode 2)
The client streams to $N_0$, which streams directly to $N_1$, which streams to $N_2$.
- **Latency**: $O(R)$ sequential hops—higher write completion latency.
- **Cost**: Every node expends exactly 1 unit of ingress and 1 unit of egress bandwidth. This avoids egress saturation on any single node, making it ideal for large sequential file transfers across bandwidth-constrained links.

---

## 2. Primary-Splay: Client-Side Fan-Out

In **Primary-Splay (Mode 3)**, the momo client calculates the CRUSH placement set locally and connects directly to all $R$ nodes in parallel.

{{< diagram src="/diagrams/02b-primary-splay.svg" alt="Primary-Splay client fan-out" caption="Figure 3: Client-side concurrent fan-out eliminates server relay bandwidth" >}}

- **Zero Server Relay Bandwidth**: Server daemons expend 0 network egress copying bytes to peers; the client network carries the fan-out.
- **Dynamic Downgrade Invariant**: When non-momo clients (like `aws-cli` or `curl`) connect, they only send a single stream. The receiving server detects this (`comm.IsExternalClient()`) and automatically steps down from `Primary-Splay` to `Splay` replication, preventing single-copy data loss.

---

## 3. The Polymorphic Controller

Replication modes are not static configuration flags; they are dynamic cluster states. In `src/server/server.go`, the cluster designates **Node 0** as the single authoritative polymorphic controller:

{{< diagram src="/diagrams/02c-polymorphic-controller.svg" alt="Polymorphic controller logic" caption="Figure 4: Real-time telemetry drives dynamic topology transitions" >}}

```go
func (s *Server) ChangeReplicationMode(newMode int) error {
    if s.id != 0 {
        return fmt.Errorf("only node 0 can orchestrate replication mode changes: %w", syscall.EPERM)
    }

    // 1. Broadcast ModeChange RPC to all cluster daemons
    for _, peer := range s.cluster.Peers() {
        if err := s.sendModeChangeRPC(peer, newMode); err != nil {
            return fmt.Errorf("failed to shift peer %d: %w", peer.ID, err)
        }
    }

    // 2. Atomically update local state
    s.currentMode.Store(int32(newMode))
    log.Printf("Polymorphic Engine: Successfully shifted cluster replication mode to %d", newMode)
    return nil
}
```

When a node receives a mode change command, it updates an atomic pointer (`atomic.Int32`). Ongoing in-flight streams continue using their initiated mode to completion, while newly accepted connections immediately adopt the new topology with zero downtime.

---

## 4. Tradeoff Analysis

| Mode | Write Latency | Primary Node Egress | Inter-Node Traffic | Client Requirements |
|---|---|---|---|---|
| **`None (0)`** | Lowest ($0\text{ hops}$) | Zero | Zero | Works with any client |
| **`Splay (1)`** | Low ($1\text{ hop}$) | High ($(R-1) \times \text{Size}$) | High | Works with any client |
| **`Chain (2)`** | High ($R\text{ hops}$) | Low ($1 \times \text{Size}$) | Distributed evenly | Works with any client |
| **`Primary-Splay (3)`** | Low ($1\text{ hop}$) | **Zero** | **Zero** | Requires momo-aware client |

---

## 5. Engineering Standards (⚡ Bolt & 🛡 Sentinel)

In accordance with [docs/STANDARDS.md](../../STANDARDS.md):
- ⚡ **Bolt**: Strategy selection is a lightweight control-plane decision. The data plane uses pooled stack buffers and bounded semaphores to ensure zero heap allocations during replication forwarding.
- 🛡 **Sentinel**: In-flight streams are never forcibly interrupted during a topology shift. Single-source authority on Node 0 prevents split-brain mode oscillations.

## Related

- [001: Origin and Genesis](001-origin-and-genesis.md)
- [003: TCP → QUIC Transport](003-transport-tcp-to-quic.md)
- [021: R3 Write Durability and Quorum](021-r3-write-durability-quorum.md)
