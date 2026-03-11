# Proposal: Implement a Peer-to-Peer (P2P) Transport Layer

- **Champion:** Gemini CLI
- **Status:** `Draft`

## 1. Problem

Momo's current architecture relies on a static configuration (`momo.conf`) to define the network topology. The logic for replication (especially in modes like Chain and Splay) involves direct, hardcoded connections between servers. Furthermore, the role of Server ID 0 as the central authority for metrics and mode changes creates a single point of failure (SPOF) and a potential performance bottleneck. If server 0 goes down, the system's dynamic capabilities are lost.

## 2. Proposed Solution

Transition `momo` from its static client-server/server-server connection model to a dynamic, decentralized peer-to-peer (P2P) transport layer. This change will serve as the new foundation for all replication strategies.

- **Decentralized Transport:** Nodes will connect to a set of bootstrap peers and then automatically discover and maintain connections with other nodes in the network.
- **Resilience:** By removing the hard dependency on a single leader node for transport, the system will be more resilient to failures.
- **Foundation for Replication:** The existing replication strategies (Chain, Splay, etc.) will be implemented *on top* of this P2P layer, rather than being intertwined with the connection logic itself.

## 3. Technical Spec

1.  **Introduce a `p2p` Package:**
    - Create a new package `src/p2p/`.
    - Define a `p2p.Transport` interface responsible for listening for, dialing, and managing connections.
    - Define a `p2p.Peer` interface representing a remote node in the network.

2.  **Implement `TCPTransport`:**
    - Create a `p2p.TCPTransport` struct that implements the `Transport` interface using standard TCP sockets. This implementation will be inspired by the `distributedfilesystemgo` project.
    - It will manage a pool of connected peers.
    - It will include a `Consume() <-chan RPC` method to deliver incoming messages to the application layer.

3.  **Refactor `server/server.go`:**
    - The main `Server` struct will be modified to use the `p2p.Transport` instead of manually dialing sockets.
    - It will maintain a peer map to track other nodes.
    - The `bootstrapNetwork()` method will be used to connect to initial seed nodes specified in the configuration.

4.  **Adapt Replication Logic:**
    - The logic for `Chain`, `Splay`, etc., will be refactored to use the peer map. For example, in Splay mode, the server will fetch peers from its peer list and broadcast the file to them, rather than connecting to statically configured hosts.

## 4. Performance Analysis & Justification

This is a foundational architectural change with significant performance implications.

-   **Expected Performance Impact:**
    1.  **Throughput Increase:** For parallelizable replication modes like `Splay` and `PrimarySplay`, performance is expected to **improve**. Direct peer-to-peer data transfer avoids routing all traffic through a single primary node, reducing bottlenecks and leveraging the full network capacity between nodes.
    2.  **Latency Introduction:** There may be a minor, one-time latency increase on startup as nodes perform handshakes and peer discovery. This is a negligible cost for long-running processes.

-   **Justification for Potential Penalties:** The primary motivation for this change is **resilience and scalability**, which are critical non-functional requirements. The risk of a minor startup latency is a worthwhile trade-off for eliminating the single point of failure. The expected improvement in parallel throughput under load is the primary performance benefit.

-   **Measurement Plan:**
    1.  **Baseline:** Run `make benchmark COUNT=10` and `make test-e2e` on the current `master` branch to establish a firm performance baseline.
    2.  **Post-Implementation:** After implementing the P2P layer, run the same benchmark and E2E tests.
    3.  **Analysis:**
        - Compare the `ns/op` for `BenchmarkConcurrentUploads`. We expect this to decrease (improve).
        - Add a new E2E test scenario that simulates the failure of a node during a replication operation to empirically prove the improved resilience.
        - The change will be considered successful if throughput is maintained or improved, and resilience is demonstrably increased.

---
