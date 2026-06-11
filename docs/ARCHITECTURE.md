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
- All communication is abstracted through a `Communicator` interface provided by the `ProtocolFactory`.

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain`, `Splay`). This logic is **completely agnostic** of the communication layer. It executes replication by requesting a connection (`Communicator`) from the factory and doesn't care whether bytes move via TCP or QUIC streams.

#### 3. State Management (Polymorphic System)
The metrics component runs on a designated server (server 0) and is responsible for monitoring system metrics (CPU and memory usage) and changing the replication strategy based on predefined thresholds. It operates independently of the network stack.

### 4. Distributed Object Engine (CAS 2.0)
Momo utilizes a **Shared-Nothing Partitioned Architecture** for its object storage layer:

- **Data Placement (CRUSH)**: We use a simplified Go implementation of the **CRUSH** (Controlled Replication Under Scalable Hashing) algorithm, originally designed by **Sage Weil** (the creator of Ceph). CRUSH allows us to calculate data locations deterministically, eliminating the need for a central metadata server or coordinator. Given a file hash and the cluster map, both the client and all nodes can calculate exactly which nodes should store the data.
- **Metadata Management (Bbolt)**: High-speed, transactional metadata is stored in local Bbolt databases on each node. Metadata is partitioned across the cluster using the same algorithmic placement as the data itself.
- **Automatic Deduplication**: By using content-addressing (SHA-256), Momo ensures that any specific piece of data is only stored once per node, regardless of the filenames associated with it.

### 5. Automated Governance & AI Reviewer
To maintain high integrity in a single-contributor environment, Momo employs an automated governance layer:
- **Gemini AI Reviewer**: A GitHub Action that uses the Gemini API to analyze PR diffs. It specifically enforces the **⚡ Bolt** (performance) and **🛡️ Sentinel** (security) patterns.
- **Project Steering Rules**: Mandatory mandates (Zero-Crash, POSIX Error Mapping) are codified in `openspec/project.md` and automatically validated by the AI Reviewer.

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

1.  **Placement Calculation**: The client hashes the file content (SHA-256) and runs the CRUSH-lite algorithm against its local Cluster Map.
2.  **Primary Selection**: The algorithm returns an ordered list of nodes. The first node is the **Primary** for this specific object.
3.  **Negotiated Transfer**: The client performs an 84-byte handshake with the Primary, providing the Content Hash and Timestamp.
4.  **Deduplication Check**: The Primary queries its local **Bbolt** instance. If the hash exists, it signals the client to skip the payload.
5.  **Algorithmic Replication**: If needed, the Primary forwards the data to the next nodes in the CRUSH list (the **Secondaries**), using the negotiated replication strategy (Chain or Splay).

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
                            +------>     Server 1    <------+
                            /                                 |
+----------------+      +----------------+      +----------------+
|                |      |                |      |                |
|     Client     +------>     Server 0   +------>     Server 2   |
|                |      |                |      |                |
+----------------+      +----------------+      +----------------+
                            \                                 ^
                            +------>      ...      <------+
```

### 4. Primary-Splay Replication

In this mode, the client sends the file to all servers in the cluster simultaneously.

```
                 +------>     Server 1    <------+
                /                                 |
+----------------+      +----------------+      +----------------+
|                |      |                |      |                |
|     Client     +------>     Server 0   <------>     Server 2   |
|                |      |                |      |                |
+----------------+      +----------------+      +----------------+
                \                                 ^
                 +------>      ...      <------+
```

## Polymorphic System

The most unique aspect of Momo is its ability to change replication strategies at runtime. The metrics component on Server 0 monitors the CPU and memory usage of the system. If the usage exceeds a certain threshold, the system will switch to a less resource-intensive replication strategy. Conversely, if the usage is low, the system will switch to a more robust replication strategy.

This allows the system to adapt to changing workloads and maintain optimal performance and data redundancy.
