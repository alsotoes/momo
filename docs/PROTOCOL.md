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
    -   **RequestedMode:** 1-byte ASCII integer representing the transaction type or replication strategy:
        -   `'0'`: **ReplicationNone** - Upload without replication.
        -   `'1'`: **ReplicationChain** - Upload using chain replication.
        -   `'2'`: **ReplicationSplay** - Upload using splay replication.
        -   `'3'`: **ReplicationPrimarySplay** - Upload using primary-splay replication.
        -   `'L'`: **ModeList** - Query directory list of stored file objects.
        -   `'D'`: **ModeDelete** - Request specific file deletion.
        -   `'G'`: **ModeGet** - Request file payload retrieval (Download).
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

### Native Directory Listing (LIST - `'L'`)

When `RequestedMode` is `ModeList` (`'L'`), the client queries the list of all file metadata stored on the server.

1.  **Handshake:** Completed with `'L'`.
2.  **Server Response (File Count):** Server writes a 4-byte big-endian integer representing the number of files:
    ```
    |-----------------|
    | File Count (4)  |
    |-----------------|
    ```
3.  **Metadata Stream:** For each file, the server streams a 192-byte metadata packet containing:
    -   **SHA-256 Checksum:** 64-byte hexadecimal string, null-padded.
    -   **File Name:** 64-byte ASCII string (including subfolders), null-padded.
    -   **File Size:** 64-byte ASCII decimal size string, null-padded.
    ```
    |-----------------|------------------|-----------------|
    |   Hash (64)     | File Name (64)   | File Size (64)  |
    |-----------------|------------------|-----------------|
    ```

### Native File Deletion (DELETE - `'D'`)

When `RequestedMode` is `ModeDelete` (`'D'`), the client requests the deletion of a specific file.

1.  **Handshake:** Completed with `'D'`.
2.  **Target Name (Client sends):** Client sends the 64-byte null-padded name of the file to delete.
3.  **Server Response (ACK):** Server deletes the mapping on BoltDB and responds with a 1-byte status code (`'0'` for success, `'1'` for error).

### Native File Retrieval (GET - `'G'`)

When `RequestedMode` is `ModeGet` (`'G'`), the client requests the raw binary payload download of a specific file.

1.  **Handshake:** Completed with `'G'`.
2.  **Target Name (Client sends):** Client sends the 64-byte null-padded name of the file to retrieve.
3.  **Server Response (ACK/Payload):**
    -   If the file does not exist, the server writes a 1-byte `'1'` (Not Found) code and closes.
    -   If the file exists, the server writes a 1-byte `'0'` (Success) code, followed by a 64-byte null-padded `FileSize` string, followed by the raw binary stream of the file until EOF.

### Payload

The file payload is streamed until EOF. The server reads exactly the number of bytes specified in the `fileSize` metadata field.

## Replication Modes in Detail

The following diagrams illustrate the message flow for each replication mode.

### No Replication

The client sends the file to the primary server, and no further replication occurs. This typically happens when the `replication_factor` is 1 or when a node is acting as the terminal destination.

```
+--------+                           +----------+
| Client |                           | Server 0 |
+--------+                           +----------+
    | --- Handshake ----------------------> |
    | <--- Replication Mode (0) ----------  |
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

## S3 Compatibility Layer & Polymorphic Routing

To provide absolute cloud-native interoperability, Momo implements an S3-compatible REST protocol gateway over the same connection port. To achieve this without breaking Momo's custom distributed replication engine or introducing bloated third-party dependencies, Momo utilizes a **Strict Gateway Interceptor Pattern** within its communication layer.

Depending on the incoming network request, the server polymorphically routes traffic under two distinct scenarios:

### Polymorphic S3 PUT Operation Versions

While a standard S3 client (such as `aws-cli`) always issues a standard, monolithic HTTP `PUT` request, Momo's S3 gateway processes this operation polymorphically under **three distinct distributed versions** depending on the active cluster replication strategy:

1. **PUT-Chain (Chain Replication):**
   - **Behavior:** Pipelined replication chain. The client uploads the file payload to the Primary node, which saves the copy and forwards it to the next node in the chain, which continues down the replication order ring sequentially.
   - **Advantage:** Zero concurrent client upload network overhead.
2. **PUT-Splay (Splay Replication):**
   - **Behavior:** Server-side splaying. The client uploads a single file payload to the Primary node. The Primary node saves this first copy and then splays (transmits) the data concurrently to all other replica nodes in the cluster in parallel.
   - **Advantage:** Client only performs a single upload stream, offloading concurrent transfers to the Primary server.
3. **PUT-PrimarySplay (Primary-Splay Replication / Client-Splay):**
   - **Behavior:** Client-side splaying. This method moves the replication logic entirely to the client. The client uses the Sage Weil CRUSH placement algorithm to connect directly to all replica nodes and copies/splays the file payload to all of them concurrently in parallel.
   - **Advantage:** Offloads the concurrent transmission workload completely from the Primary server to the client, preserving server CPU/network resources under heavy load.

These versions are swapped completely on-the-fly by Momo's polymorphic metric-monitoring engine with **zero configuration changes on the S3 client side** and **zero downtime**.

### Scenario A: Standard S3 Client (e.g., aws-cli, boto3)

When a standard S3 tool connects to Momo, it communicates via raw, standard S3 HTTP requests. The server intercepts these requests and bypasses the Momo-specific replication pipeline entirely.

```
+---------------+                    +---------------+                    +-------------+
| Standard S3   |                    | S3Communicator|                    | Local Bbolt |
| Client        |                    | (Server Side) |                    | Database    |
+---------------+                    +---------------+                    +-------------+
        |                                    |                                   |
        | ----- GET /?list-type=2 ---------> |                                   |
        |       (ListObjectsV2)              |                                   |
        |                                    | ----- store.List() -------------> |
        |                                    | <---- File list ----------------- |
        | <---- 200 OK (S3 XML) ------------ |                                   |
        |       (Gracefully Closes)          |                                   |
        |                                    | (Bypasses custom Momo replication)|
```

**Step-by-step Flow:**
1.  **Request Arrival:** The standard client makes an S3 request (e.g., `GET /?list-type=2` for listing, `GET /bucket/file.txt` for downloads, or `DELETE /bucket/file.txt` for deletion) containing standard AWS-HMAC-SHA256 headers.
2.  **Handshake Interception:** The server accepts the socket and calls `comm.HandshakeServer(expectedAuthToken)`. S3Communicator reads the HTTP request, parses and validates the token.
3.  **REST Query Routing:** Because the request method is `GET` or `DELETE`, S3Communicator detects it as a REST query and **bypasses standard Momo framing**:
    -   **ListObjectsV2:** Queries `store.List()`, formats the file list into S3-compliant XML using a high-performance allocation-free `bytes.Buffer`, writes `200 OK` back to the client, and returns `ErrRequestHandled`.
    -   **GetObject:** Queries `store.Get(key)`, streams the binary content directly to the client, and returns `ErrRequestHandled`.
    -   **DeleteObject:** Invokes `store.Delete(key)` on BoltDB, writes a `204 No Content` response, and returns `ErrRequestHandled`.
4.  **Graceful Termination:** Upon receiving the `ErrRequestHandled` sentinel error from the handshake, the server daemon disables Momo replication acknowledgements (ACKs) and immediately closes the connection gracefully. The S3 client receives standard HTTP bytes and never sees custom Momo handshakes.

### Scenario B: Momo Server Peer (Inter-Node Replication)

When a Momo cluster node acts as an S3 client to forward and replicate files to another node (such as under `Chain` or `Splay` mode), it uses standard HTTP `PUT` but embeds custom **Momo-specific handshake headers**.

```
+---------------+                    +---------------+                    +-------------+
| Momo Client   |                    | S3Communicator|                    | Server      |
| Node (Peer)   |                    | (Server Side) |                    | Daemon      |
+---------------+                    +---------------+                    +-------------+
        |                                    |                                   |
        | ----- PUT /file.txt -------------> |                                   |
        |       X-Momo-Requested-Mode: 2     |                                   |
        |       X-Momo-Timestamp: 123...     |                                   |
        |                                    |                                   |
        |                                    | ----- Handshake Success --------> |
        | <---- Final Mode (Confirmed) ----- |                                   |
        |                                    |                                   |
        |                                    | (Proceeds to Metadata/Payload     |
        |                                    |  replication handshake pipeline)  |
```

**Step-by-step Flow:**
1.  **Request Arrival:** The peer node makes an HTTP `PUT` request but includes the custom headers `X-Momo-Requested-Mode` and `X-Momo-Timestamp`.
2.  **Replication Identification:** Inside `HandshakeServer`, the communicator detects that the HTTP method is `PUT` (a write/replicate request). It parses the requested replication mode and timestamp from the headers.
3.  **Momo Handshake Execution:** S3Communicator validates the credentials and returns the replication mode and timestamp without triggering any REST interception.
4.  **Framing Alignment:** Because the handshake completed normally with a `nil` error, the server daemon continues standard Momo framing over the open stream:
    -   Server transmits the final negotiated replication mode.
    -   Server expects and receives custom file metadata.
    -   Server executes CAS deduplication checking and payload streaming.
    -   Server transmits the final Momo replication acknowledgment (`ACK`).

## Security & Resilience

-   **Authentication:** Every connection requires a valid 64-byte AuthToken.
-   **Timeouts:** Connections are protected by rolling idle timeouts (30s) and phased absolute deadlines (10s for handshake, 60s for metadata) to prevent Slowloris attacks.
-   **Sanitization:** All network inputs and error messages are sanitized before logging to prevent CRLF injection.
-   **Error Handling:** If an error occurs (e.g., hash mismatch, disk full, or connection reset), the connection is closed. Hash mismatches return `EBADMSG`.

## P2P Gossip Protocol

When P2P is enabled, nodes exchange gossip membership and failure detection RPCs over a separate port (default 4450). All RPCs use a binary, length-prefixed frame format:

```
[4 bytes: total length] [1 byte: msg type] [4 bytes: from ID] [N bytes: payload]
```

### Message Types

| Type | Value | Description |
|------|-------|-------------|
| `MsgHeartbeat` | 1 | Periodic heartbeat with sender's peer list |
| `MsgMembership` | 2 | Node join/leave announcement |
| `MsgSuspect` | 3 | Suspicion announcement about a peer |
| `MsgQuery` | 4 | Scatter-gather query request |
| `MsgQueryResponse` | 5 | Scatter-gather query response |
| `MsgLeaseRequest` | 6 | Lease request for consensus |
| `MsgLeaseGrant` | 7 | Lease grant or deny response |
| `MsgLeaseRelease` | 8 | Lease release notification |
| `MsgPing` | 9 | Direct ping for SWIM failure detection |
| `MsgAck` | 10 | Ack response to a ping |
| `MsgIndirectPing` | 11 | Indirect ping request via intermediary |

### Ping Payload (MsgPing / MsgAck / MsgIndirectPing)

```
[8 bytes: ping ID] [4 bytes: target ID] [8 bytes: timestamp unixnano]
```

- **PingID**: Unique identifier for matching acks to pings
- **TargetID**: The peer being pinged (for indirect pings, the ultimate target)
- **Timestamp**: Send time for RTT calculation
