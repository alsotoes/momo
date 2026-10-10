---
title: "Replication Strategies and the Polymorphic Engine"
date: 2026-05-04T18:54:37Z
draft: false
post_type: architecture
tags: [go, replication, architecture, bolt, sentinel]
categories: [origin]
summary: "Chain, Splay, and Primary-Splay replication — and the metrics-driven controller that switches between them at runtime."
artifacts:
  - {type: doc, path: docs/REFERENCE/REPLICATION_STRATEGIES.md}
  - {type: spec, path: openspec/changes/add-replication-durability-floor}
related:
  - 001-origin-and-genesis
  - 003-transport-tcp-to-quic
  - 021-r3-write-durability-quorum
  - 053-multitenancy-authorization-audit
---

Most distributed storage architectures lock their replication topology into
their core design: Ceph peers a primary OSD with replicas, Cassandra has clients
fan out to coordinators, and HDFS pipelines a daisy-chain of DataNodes. Each
topology optimizes for one constraint at the expense of the others — low latency,
low network egress, or even bandwidth distribution.

momo was designed around an alternative premise: **why hardcode a single
replication topology when network conditions, hardware heterogeneity, and client
types shift over time?** The **Polymorphic Engine** lets a running momo cluster
change its active replication strategy without restarting daemons or dropping
in-flight connections.

Here, *replication* simply means keeping multiple copies of each object across
different machines, and *replication factor* (written **R** below) is how many
copies we keep. The interesting question is the shape of the write path that
creates those copies.

{{< diagram src="/diagrams/02-replication-strategies.svg" alt="Replication strategies overview" caption="Figure 1: High-level overview of momo's four replication strategies" >}}

---

## 1. The Core Topologies: Splay vs. Chain

momo formalizes four replication modes:

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

The client sends one stream to the primary node, call it the first node. That
node writes the payload to its local blob store while simultaneously fanning out
concurrent streams to the secondary replicas. The shape is a fan or "splay": one
inbound stream becomes several outbound streams.

- **Latency:** one network hop — the fastest write latency.
- **Cost:** the primary node carries `(R - 1) × payload` of network egress
  bandwidth, creating a bottleneck on the primary's network card.

### Chain Replication (Mode 2)

The client streams to the first node, which streams directly to the second,
which streams to the third. Each node forwards to exactly one successor.

- **Latency:** R sequential hops — higher write-completion latency, because the
  write is not durable until it reaches the tail.
- **Cost:** every node spends exactly one unit of ingress and one unit of egress
  bandwidth. No single node's egress saturates, which makes chain the right
  choice for large sequential transfers across bandwidth-constrained links.

---

## 2. Primary-Splay: Client-Side Fan-Out

In **Primary-Splay (Mode 3)**, the momo client computes the replica set locally
(via CRUSH-lite, [005](005-crush-placement.md)) and connects directly to all R
nodes in parallel.

{{< diagram src="/diagrams/02b-primary-splay.svg" alt="Primary-Splay client fan-out" caption="Figure 3: Client-side concurrent fan-out eliminates server relay bandwidth" >}}

- **Zero server relay bandwidth.** Daemons spend no egress copying bytes to
  peers; the client's network carries the fan-out.
- **Dynamic downgrade invariant.** When a non-momo client (say `aws-cli` or
  `curl`) connects, it sends only a single stream. The receiving server detects
  that the peer is external and automatically steps down from Primary-Splay to
  Splay, so the write still gets replicated instead of silently landing as one
  copy.

---

## 3. The Polymorphic Controller

Replication modes are not static configuration flags; they are dynamic cluster
states. One node — node 0 — is the single authoritative controller. It is the
only node allowed to decide and broadcast a topology change, which prevents two
nodes from racing to reconfigure the cluster.

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

When a node receives a mode change, it updates an atomic integer holding the
current mode. Ongoing in-flight streams keep using the mode they started with
and run to completion; newly accepted connections immediately adopt the new
topology. The result is a topology shift with zero downtime and no dropped
transfers.

---

## 4. Tradeoff Analysis

| Mode | Write Latency | Primary Node Egress | Inter-Node Traffic | Client Requirements |
|---|---|---|---|---|
| **`None (0)`** | Lowest (no hops) | Zero | Zero | Works with any client |
| **`Splay (1)`** | Low (one hop) | High (`(R-1) × payload`) | High | Works with any client |
| **`Chain (2)`** | High (R hops) | Low (`1 × payload`) | Distributed evenly | Works with any client |
| **`Primary-Splay (3)`** | Low (one hop) | **Zero** | **Zero** | Requires momo-aware client |

The trade-off is genuine: Primary-Splay is the cheapest and fastest, but it
requires clients that speak momo's protocol. Splay is universal but makes the
primary's NIC the bottleneck. Chain is the most bandwidth-fair but the slowest
to acknowledge. The Polymorphic Engine exists so the cluster does not have to
pick one forever.

---

## 5. Engineering Standards (⚡ Bolt & 🛡 Sentinel)

In accordance with [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- ⚡ **Bolt**: strategy selection is a lightweight control-plane decision. The
  data plane uses pooled stack buffers and bounded semaphores so replication
  forwarding adds no heap allocations.
- 🛡 **Sentinel**: in-flight streams are never forcibly interrupted during a
  topology shift, and single-source authority on node 0 prevents split-brain
  mode oscillations.

## References / Dig deeper

- Replication design doc: [docs/REFERENCE/REPLICATION_STRATEGIES.md](../../REPLICATION_STRATEGIES.md).
- Spec: `openspec/changes/add-replication-durability-floor`.
- Sibling posts: [001: Origin and Genesis](001-origin-and-genesis.md),
  [003: TCP → QUIC Transport](003-transport-tcp-to-quic.md),
  [021: R3 Write Durability and Quorum](021-r3-write-durability-quorum.md).
- CRUSH-lite placement: [005](005-crush-placement.md).
