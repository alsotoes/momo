# Momo Wire Protocol

This document provides a detailed description of the Momo wire protocol. It is intended for developers who want to understand the network interactions between the client and servers or build a compatible client in another language.

## Overview

The Momo protocol is a simple, TCP-based protocol for file replication. It consists of a handshake, metadata exchange, and a file transfer phase. The protocol is designed to be lightweight and efficient, with a focus on minimizing overhead through zero-allocation techniques.

## Handshake

The handshake is initiated by the client and is used to authenticate the connection and establish the replication mode.

1.  The client opens a TCP connection to the primary server (usually server 0).
2.  The client sends a combined authentication and timestamp packet:
    -   **AuthToken:** 64-byte string, null-padded.
    -   **Timestamp:** 19-byte ASCII string (e.g., `UnixNano`).
3.  The server validates the AuthToken using constant-time comparison.
4.  The server decides which replication mode to use for this connection based on its current configuration and metrics.
5.  The server responds with an ASCII-encoded integer representing the chosen replication mode (e.g., `4` for Primary-Splay).

**Handshake Layout:**

```
|-----------------|-----------------|
|  AuthToken (64) | Timestamp (19)  |
|-----------------|-----------------|
```

**Replication Mode Codes:**

-   `1`: Chain Replication
-   `2`: Splay Replication
-   `3`: Primary-Splay Replication
-   `4`: No Replication (Fallback/Default)

## Message Framing

Once the handshake is complete, the client sends the file metadata, followed by the file payload.

### Metadata

The metadata consists of three fixed-size fields:

-   **SHA-256 Checksum:** 64-byte hexadecimal string.
-   **File Name:** 64-byte ASCII string, null-padded (`\x00`).
-   **File Size:** 64-byte ASCII string representing the decimal file size, null-padded (`\x00`).

**Layout:**

```
|-----------------|------------------|-----------------|
|   Hash (64)     | File Name (64)   | File Size (64)  |
|-----------------|------------------|-----------------|
```

### Payload

The file payload is streamed until EOF. The server reads exactly the number of bytes specified in the `fileSize` metadata field.

## Replication Modes in Detail

The following diagrams illustrate the message flow for each replication mode.

### No Replication

The client sends the file to the primary server, and no further replication occurs.

```
+--------+                           +----------+
| Client |                           | Server 0 |
+--------+                           +----------+
    | --- Handshake ----------------------> |
    | <--- Replication Mode (4) ----------  |
    | --- Metadata & Payload ----------->   |
    | <--- ACK0 --------------------------  |
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

## Security & Resilience

-   **Authentication:** Every connection requires a valid 64-byte AuthToken.
-   **Timeouts:** Connections are protected by rolling idle timeouts (30s) and phased absolute deadlines (10s for handshake, 60s for metadata) to prevent Slowloris attacks.
-   **Sanitization:** All network inputs and error messages are sanitized before logging to prevent CRLF injection.
-   **Error Handling:** If an error occurs (e.g., hash mismatch, disk full, or connection reset), the connection is closed. Hash mismatches return `EBADMSG`.
