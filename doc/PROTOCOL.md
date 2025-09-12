# Momo Wire Protocol

This document provides a detailed description of the Momo wire protocol. It is intended for developers who want to understand the network interactions between the client and servers or build a compatible client in another language.

## Overview

The Momo protocol is a simple, TCP-based protocol for file replication. It consists of a handshake, metadata exchange, and a file transfer phase. The protocol is designed to be lightweight and efficient, with a focus on minimizing overhead.

## Handshake

The handshake is initiated by the client and is used to establish the replication mode for the current connection.

1.  The client opens a TCP connection to the primary server (usually server 0).
2.  The client sends a 19-byte ASCII timestamp (e.g., `UnixNano`).
3.  The server receives the timestamp and decides which replication mode to use for this connection. This decision is based on the server's current configuration and metrics.
4.  The server responds with a single-digit ASCII code representing the chosen replication mode:

    -   `1`: No Replication
    -   `2`: Chain Replication
    -   `3`: Splay Replication
    -   `4`: Primary-Splay Replication

**Diagram:**

```
+--------+                           +----------+
| Client |                           | Server 0 |
+--------+                           +----------+
    |                                      |
    | --- (1) TCP Connection Open -------->|
    |                                      |
    | --- (2) Send 19-byte Timestamp ----> |
    |                                      |
    | <--- (4) Receive 1-byte Rep Mode ----|
    |                                      |
```

## Message Framing

Once the handshake is complete, the client sends the file metadata, followed by the file payload.

### Metadata

The metadata consists of three fixed-size fields:

-   **MD5 Checksum:** 32-byte hexadecimal string.
-   **File Name:** 64-byte ASCII string, right-padded with colons (`:`).
-   **File Size:** 64-byte ASCII string representing the decimal file size, right-padded with colons (`:`).

**Layout:**

```
|-----------------|------------------|-----------------|
|   MD5 (32)      | File Name (64)   | File Size (64)  |
|-----------------|------------------|-----------------|
```

### Payload

The file payload is streamed in chunks of 1024 bytes. The server reads exactly the number of bytes specified in the `fileSize` metadata field.

## Replication Modes in Detail

The following diagrams illustrate the message flow for each replication mode.

### No Replication

The client sends the file to the primary server, and no further replication occurs.

```
+--------+                           +----------+
| Client |                           | Server 0 |
+--------+                           +----------+
    | --- Handshake ----------------------> |
    | --- Metadata & Payload ----------->   |
    | <--- ACK ---------------------------  |
```

### Chain Replication

The client sends the file to Server 0, which then replicates it to Server 1, which in turn replicates it to Server 2.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Server 0 |     | Server 1 |     | Server 2 |
+--------+     +----------+     +----------+     +----------+
    | ------------> |                |                |
    |               | -------------> |                |
    |               |                | -------------> |
```

### Splay Replication

The primary server replicates the file to all other servers in the cluster concurrently.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Server 0 |     | Server 1 |     | Server 2 |
+--------+     +----------+     +----------+     +----------+
    | ------------> |                |               |
    |               | -------------> |               |
    |               | -----------------------------> |
```

### Primary-Splay Replication

The client sends the file to all servers in the cluster concurrently.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Server 0 |     | Server 1 |     | Server 2 |
+--------+     +----------+     +----------+     +----------+
    | ------------> |              |              |
    | ---------------------------> |              |
    | ----------------------------------------->  |
```

## Error Handling

The current implementation of the Momo protocol has minimal error handling. In most cases, if an error occurs during the transfer (e.g., a network error or a mismatched MD5 checksum), the connection is closed, and the program exits with an error code. There is no mechanism for resuming a failed transfer.