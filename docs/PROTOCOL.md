# Momo Wire Protocol

This document provides a detailed description of the Momo wire protocol. It is intended for developers who want to understand the network interactions between the client and servers or build a compatible client in another language.

## Overview

The Momo protocol is a high-performance, transport-agnostic protocol for file replication. While it originated as a TCP protocol (`momo-tcp`), the architecture has been generalized via a `Communicator` interface, enabling identical application-layer semantics over QUIC streams (`momo-quic`) via `quic-go`.

It consists of a handshake, metadata exchange, and a file transfer phase. The protocol is designed to be lightweight and efficient, with a focus on minimizing overhead through zero-allocation techniques.

## Transport Independence

Whether running over raw TCP sockets or encrypted UDP QUIC streams, the byte-level protocol remains identical. For `momo-quic`, TLS 1.3 is automatically configured with self-signed certificates for node-to-node security, and a dedicated, isolated stream is opened for each client transaction.

## Handshake

The handshake is initiated by the client and is used to authenticate the connection and establish the replication mode.

1.  **Transport Connection**: The client opens a network connection (TCP socket, QUIC stream, or S3 HTTP session).
2.  **Handshake Packet**: The client sends a combined authentication, timestamp, and mode packet (84 bytes):
    -   **AuthToken:** 64-byte string, null-padded.
    -   **Timestamp:** 19-byte ASCII string (e.g., `UnixNano`).
    -   **RequestedMode:** 1-byte ASCII integer (e.g. `0` for auto-select, `1` for Chain).
3.  **Validation**: The server validates the AuthToken using constant-time comparison.
4.  **Negotiation**: 
    - If it's a new client connection, the server selects the mode based on polymorphic metrics.
    - If it's a forwarded connection between nodes, the server respects the requested mode to ensure cluster consistency.
5.  **Confirmation**: The server responds with a 1-byte ASCII-encoded integer representing the final replication mode.

**Handshake Layout (84 bytes):**

```
|-----------------|-----------------|------|
|  AuthToken (64) | Timestamp (19)  | M (1)|
|-----------------|-----------------|------|
```

## Message Framing

Once the handshake is complete, the client sends the file metadata and waits for a status code before sending the payload.

### Metadata & Deduplication Check

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

**Deduplication Flow:**

1.  After sending the metadata, the client waits for a **1-byte Status Code**.
2.  **`1` (MetadataStatusSendPayload)**: Server does not have the content. Client must stream the payload.
3.  **`2` (MetadataStatusSkipPayload)**: Server already has the content (**CAS Hit**). Client skips the payload phase and waits for the final ACK.

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

The data follows an ordered path determined by the CRUSH placement list.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Primary  |     | Second 1 |     | Second 2 |
+--------+     +----------+     +----------+     +----------+
    | ------------> |                |                |
    |               | -------------> |                |
    |               |                | -------------> |
```

### Splay Replication

The primary server replicates the file to all other servers in the placement list concurrently.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Primary  |     | Second 1 |     | Second 2 |
+--------+     +----------+     +----------+     +----------+
    | ------------> |                |               |
    |               | -------------> |               |
    |               | -----------------------------> |
```

### Primary-Splay Replication

The client sends the file to all servers in the placement list concurrently.

```
+--------+     +----------+     +----------+     +----------+
| Client |     | Primary  |     | Second 1 |     | Second 2 |
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
