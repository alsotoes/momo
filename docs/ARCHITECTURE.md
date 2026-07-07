# Momo Architecture

This document provides a high-level overview of the Momo architecture, its components, and the replication strategies it supports.

## System Overview

Momo is a TCP-based file replication system that consists of a client and a cluster of servers. The client sends files to the servers, and the servers replicate the files to each other based on a configured replication strategy. The system is designed to be polymorphic, meaning it can change its replication strategy at runtime based on system metrics.

### Components

The system is organized into a three-layer architecture to ensure clean separation of concerns and a pluggable transport mechanism:

#### 1. Communication Layer (Transport & App Protocol)
This layer handles the physical movement of bytes. It includes the carrier transport (e.g., TCP or UDP) and application-level framing.
- **Momo-TCP**: The legacy standard transport.
- **Momo-QUIC**: The modern, secure-by-default transport using `quic-go`.
- **S3 Compatibility**: An S3-compatible application layer (over TCP or QUIC) acting as a distributed gateway for cloud-native tools. This leverages the decoupled architecture from [Issue #131](https://github.com/alsotoes/momo/issues/131) and is tracked in [Issue #133](https://github.com/alsotoes/momo/issues/133).
  - **Custom Lightweight Gateway Design & Rationale (Issue #225)**: Instead of integrating a third-party open-source S3 server engine (such as MinIO or GoFakeS3) which would introduce dozens of heavy dependency packages and violate Momo's performance (**⚡ Bolt**) and security (**🛡️ Sentinel**) paradigms, Momo implements its own zero-dependency S3 REST adapter. This maintains a small binary surface, protects against third-party supply-chain vulnerabilities, and integrates seamlessly with both standard TCP sockets and secure **QUIC streams**.
  - **REST Query Interception Model**: Standard S3 REST commands like `GET /` (ListObjectsV2), `GET /key` (GetObject), and `DELETE /key` (DeleteObject) are intercepted directly at the HTTP layer inside `S3Communicator.HandshakeServer`.
  - **Zero-Bypass Control Flow**: Upon receiving a REST command, `S3Communicator` queries or updates our BoltDB storage layer directly and streams the S3-compliant HTTP response straight back to the client socket. It then returns a special `ErrRequestHandled` transport sentinel to the server daemon, allowing the daemon to gracefully close the connection without triggering unnecessary inter-node replication loops or replication acknowledgements (ACKs).
  - **High-Performance XML Serialization**: S3 XML responses (such as `<ListBucketResult>`) are formatted manually using `bytes.Buffer` and custom character escape sequences rather than slow, reflection-based XML serialization. This ensures sub-millisecond listing responses and conforms perfectly with the low-allocation **⚡ Bolt** standard.
- All communication is abstracted through a `Communicator` interface provided by the `ProtocolFactory`.
- **Connection Idle Timeout & POSIX Mapping**: To defend against Slowloris attacks and resource-exhaustion conditions, both inbound and outbound network connections are wrapped with an `IdleTimeoutConn` that enforces a rolling idle deadline. In alignment with POSIX standards, any socket read or write operation that times out is intercepted and wrapped to explicitly propagate a `syscall.ETIMEDOUT` error.

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain`, `Splay`). This logic is **completely agnostic** of the communication layer. It executes replication by requesting a connection (`Communicator`) from the factory and doesn't care whether bytes move via TCP or QUIC streams.

#### 3. State Management (Polymorphic System)
The metrics component runs on every node. It is responsible for monitoring local system metrics (CPU and memory usage). When a threshold is reached, the node broadcasts the new replication strategy to the entire cluster via the `ChangeReplication` endpoint, ensuring all potential "Primary" nodes remain in sync.

### 4. Distributed Object Engine (CAS 2.0)
Momo utilizes a **Shared-Nothing Partitioned Architecture** for its object storage layer, encapsulated in the `src/storage` package:

- **Data Placement (CRUSH)**: We use a simplified Go implementation of the **CRUSH** (Controlled Replication Under Scalable Hashing) algorithm, originally designed by **Sage Weil** (the creator of Ceph). CRUSH allows us to calculate data locations deterministically, eliminating the need for a central metadata server or coordinator. Given a file hash and the cluster map, both the client and all nodes can calculate exactly which nodes should store the data.
- **Metadata Management (Bbolt)**: High-speed, transactional metadata is stored in local Bbolt databases on each node. Metadata is partitioned across the cluster using the same algorithmic placement as the data itself.
- **Automatic Deduplication**: By using content-addressing (SHA-256), Momo ensures that any specific piece of data is only stored once per node, regardless of the filenames associated with it.

### 5. Automated Governance & AI Reviewer
To maintain high integrity in a single-contributor environment, Momo employs an automated governance layer:
- **Gemini AI Reviewer**: A GitHub Action that uses the Gemini API to analyze PR diffs. It specifically enforces the **⚡ Bolt** (performance) and **🛡️ Sentinel** (security) patterns.
- **Project Steering Rules**: Mandatory mandates (Zero-Crash, POSIX Error Mapping) are codified in the `context` section of `openspec/config.yaml` and automatically validated by the AI Reviewer.

### 6. Verification & Quality Assurance
The system is backed by a multi-stage automated testing pipeline:
- **Distributed Simulation**: End-to-end smoke tests simulate various cluster sizes (up to 5 nodes) and protocols.
- **Placement Validation**: Automated checks verify that the CRUSH algorithm distributes data correctly and respects the `replication_factor`.
- **Integrity Checks**: Every test suite verifies data consistency and metadata accuracy across all participating nodes.

## High-Level Architecture

The system uses Sage Weil's **CRUSH algorithm** (simplified Go implementation) to distribute load across all available nodes. There is no single "entry point" or central coordinator; instead, the client deterministically selects the optimal primary node for each object based on its content hash.

```
                         +--------------------------+
                         |          Client          |
                         | (Calculates CRUSH Map)   |
                         +------------+-------------+
                                      |
                +---------------------+---------------------+
                |                     |                     |
                v                     v                     v
         +------+------+       +------+------+       +------+------+
         |   Server A  |       |   Server B  |       |   Server C  |
         | (Local Bbolt)|       | (Local Bbolt)|       | (Local Bbolt)|
         +------+------+       +------+------+       +------+------+
                |                     |                     |
                +----------+----------+----------+----------+
                           |                     |
                           v                     v
                    Replication (Agnostic of Transport)
```

**Data Flow:**

1.  **Placement Calculation**: The client hashes the file content (SHA-256) and runs the CRUSH-lite algorithm against its local Cluster Map and the configured **`replication_factor`**.
2.  **Primary Selection**: The algorithm returns an ordered list of `n` nodes (where `n = min(factor, nodes)`). The first node is the **Primary** for this specific object.
3.  **Negotiated Transfer**: The client performs an 84-byte handshake with the Primary, providing the Content Hash, Timestamp, and the intended replication mode.
4.  **Deduplication Check**: The Primary queries its local **Bbolt** instance. If the hash exists, it signals the client to skip the payload.
5.  **Algorithmic Replication**: If needed, the Primary forwards the data to the subsequent nodes in the CRUSH list (the **Secondaries**), continuing until the number of physical copies reaches the durability goal.

## Replication Strategies

Momo supports four different replication strategies:

### 1. No Replication

In this mode, the file is only stored on the server that receives it from the client. No replication occurs.

```
+----------------+      +----------------+
|                |      |                |
|     Client     +------>     Server 0   |
|                |      |                |
+----------------+      +----------------+
```

### 2. Chain Replication

In chain replication, the servers are organized in a chain. The client sends the file to the first server in the chain. The first server then replicates the file to the second server, the second to the third, and so on.

```
+----------------+      +----------------+      +----------------+      +----------------+
|                |      |                |      |                |      |                |
|     Client     +------>     Server 0   +------>     Server 1   +------>     Server 2   |
|                |      |                |      |                |      |                |
+----------------+      +----------------+      +----------------+      +----------------+
```

### 3. Splay Replication

In splay replication, the primary server (Server 0) sends the file to all other servers in the cluster simultaneously.

```
                            +------>     Server 1
                           /
+----------------+      +----------------+
|                |      |                |
|     Client     +------>     Server 0   +------>     Server 2
|                |      |                |
+----------------+      +----------------+
                           \
                            +------>      ...
```

### 4. Primary-Splay Replication

In this mode, the client sends the file to all servers in the cluster simultaneously, distributing the replication load.

```
                            +------>     Server 0
                           /
+----------------+      +---------->     Server 1
|                |     /
|     Client     +----+
|                |     \
+----------------+      +---------->     Server 2
                           \
                            +------>      ...
```

## Polymorphic System: Dual-Dimensional Adaptability

The defining feature of Momo is its **Dual-Dimensional Polymorphic Architecture**, which enables the system to adapt dynamically to load conditions and traffic origins with **zero manual configuration changes and zero runtime impact**:

### 📈 Dimension 1: Dynamic Replication Polymorphism (Runtime Adaptation)
Momo monitors local CPU and Memory metrics continuously on every node. 
- **Under Surge Load:** If system metrics exceed specified thresholds (e.g., 80% usage), nodes coordinate to dynamically shift the cluster replication mode to a lower-overhead strategy (such as **No Replication** or **Primary-Splay**) to prevent bottleneck queues and protect cluster stability.
- **Under Low Load:** When resource usage settles below thresholds (e.g., 20% usage), the system automatically promotes the mode to highly consistent, durable strategies (like **Chain** or **Splay**), optimizing data safety.
- **Decentralized Execution:** This state change is broadcast dynamically to all potential "Primary" nodes via the `ChangeReplication` endpoint, keeping the cluster seamlessly in sync without a single point of failure.

### 🔌 Dimension 2: Wire Protocol Polymorphism (Chameleon Routing)
Momo servers listen on the exact same port (e.g., `4440`) and accept standard TCP connections or secure QUIC streams, adapting the wire framing dynamically depending on the incoming client structure:
- **Standard S3 Clients (`aws-cli`, `boto3`):** Momo acts as a pure, standard-compliant S3/Ceph REST gateway. The communicator intercepts REST operations (`GET`, `DELETE`), processes the database operations, and streams standard S3 HTTP/XML data directly back to the client socket, gracefully exiting the session using the `ErrRequestHandled` sentinel without running any custom inter-node replication procedures.
- **Momo Peer Nodes (Inter-Node Replication):** Momo acts as a highly synchronized, transactional replication engine. It detects custom handshake headers (`X-Momo-Requested-Mode`, `X-Momo-Timestamp`) inside `PUT` writes, executes our multi-stage replication framing (deduplication check, metadata verification, cluster-wide payload streaming), and transmits replication acknowledgements (`ACK` packets).

This dual-dimensional polymorphism permits Momo to simultaneously serve cloud-native clients and peer replication rings over a single port, delivering top-tier performance (**⚡ Bolt**) and robust security (**🛡️ Sentinel**) dynamically.
