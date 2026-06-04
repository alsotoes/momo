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
- **S3 Compatibility (Upcoming)**: An S3-compatible application layer (over TCP or QUIC) acting as a distributed gateway for cloud-native tools. This leverages the decoupled architecture from [Issue #131](https://github.com/alsotoes/momo/issues/131) and is tracked in [Issue #133](https://github.com/alsotoes/momo/issues/133).
- All communication is abstracted through a `Communicator` interface provided by the `ProtocolFactory`.

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain`, `Splay`). This logic is **completely agnostic** of the communication layer. It executes replication by requesting a connection (`Communicator`) from the factory and doesn't care whether bytes move via TCP or QUIC streams.

#### 3. State Management (Polymorphic System)
The metrics component runs on a designated server (server 0) and is responsible for monitoring system metrics (CPU and memory usage) and changing the replication strategy based on predefined thresholds. It operates independently of the network stack.

## High-Level Architecture

The following diagram illustrates the high-level architecture of the system:

```
+----------------+      +----------------+      +----------------+
|                |      |                |      |                |
|     Client     +------>    Server 0    +------>     Server 1   |
|                |      |      (Metrics) |      |                |
+----------------+      +----------------+      +----------------+
                                |                     |
                                v                     v
                         +----------------+      +----------------+
                         |                |      |                |
                         |    Server 2    |      |      ...       |
                         |                |      |                |
                         +----------------+      +----------------+
```

**Data Flow:**

1.  The client sends a file to Server 0.
2.  Server 0 receives the file and, depending on the current replication strategy, replicates it to other servers in the cluster.
3.  The metrics component on Server 0 monitors the system and can trigger a change in the replication strategy.

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
