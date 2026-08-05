# Momo Wire Protocol

This document provides a detailed description of the Momo wire protocol. It is intended for developers who want to understand the network interactions between the client and servers or build a compatible client in another language.

## Overview

The Momo protocol is a high-performance, transport-agnostic protocol for file replication. While it originated as a TCP protocol (`momo-tcp`), the architecture has been generalized via a `Communicator` interface, enabling identical application-layer semantics over QUIC streams (`momo-quic`) via `quic-go`.

It consists of a handshake, metadata exchange, and a file transfer phase. The protocol is designed to be lightweight and efficient, with a focus on minimizing overhead through zero-allocation techniques.

## Transport Independence

Whether running over raw TCP sockets or encrypted UDP QUIC streams, the byte-level protocol remains identical. For `momo-quic`, TLS 1.3 is automatically configured with self-signed certificates for node-to-node security, and a dedicated, isolated stream is opened for each client transaction.

## Transport TLS (Phase 1 — E2EE)

When `tls_cert` and `tls_key` are configured, TCP-based protocols (`momo-tcp`, `s3-tcp`) wrap connections in TLS 1.2+ before any application data is exchanged. QUIC protocols already use TLS 1.3 via QUIC.

QUIC peer verification defaults to strict: either `ca_cert` must be configured or `tls_insecure = true` must be explicitly set. This prevents accidental MitM vulnerabilities.

When TLS is enabled, the momo handshake uses **challenge-response authentication** instead of sending the auth token in plaintext.

## Handshake

The handshake is initiated by the client and is used to authenticate the connection and establish the replication mode.

### Plaintext Mode (default, backward compatible)

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
        -   `'O'`: **ModeOPRFEval** - Request a threshold-OPRF evaluation of a blinded dedup tag (confidential dedup).
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

### Challenge-Response Mode (when TLS is enabled)

1.  **Transport Connection**: TLS handshake completes first.
2.  **Handshake Packet**: The client sends timestamp + mode (20 bytes, no auth token):
    -   **Timestamp:** 19-byte ASCII string.
    -   **RequestedMode:** 1-byte.
3.  **Challenge**: The server generates a 32-byte cryptographically secure random nonce and sends it to the client.
4.  **Response**: The client computes `HMAC-SHA256(PadString(authToken, 64), nonce)` and sends the 32-byte response.
5.  **Validation**: The server computes the expected HMAC using its stored token and compares using `hmac.Equal` (constant-time). The auth token is **never transmitted**.
6.  **Peer Detection**: The server also checks `HMAC-SHA256(DerivePeerToken(authToken), nonce)` to distinguish peer connections from client connections.
7.  **Confirmation**: The server responds with a 1-byte replication mode.

**Challenge-Response Handshake Layout:**

```
Client → Server:  | Timestamp (19) | M (1) |         (20 bytes)
Server → Client:  | Nonce (32) |                          (32 bytes)
Client → Server:  | HMAC-SHA256 Response (32) |           (32 bytes)
Server → Client:  | Replication Mode (1) |                (1 byte)
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

When `RequestedMode` is `ModeList` (`'L'`), the client queries the list of all file metadata stored on the server. This operation is restricted to peer connections only (authenticated with the derived peer token) to prevent file enumeration by direct clients (CVE-001). Non-peer connections receive a 0-count empty list.

1.  **Handshake:** Completed with `'L'`. Must use the derived peer token.
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

When `RequestedMode` is `ModeDelete` (`'D'`), the client requests the deletion of a specific file. The client must provide a proof-of-knowledge (content hash) to prevent unauthorized file deletion by name alone (CVE-003).

1.  **Handshake:** Completed with `'D'`.
2.  **Target Name + Hash Proof (Client sends):** Client sends a 128-byte request containing:
    -   **File Name:** 64-byte ASCII string, null-padded (`\x00`).
    -   **Content Hash:** 64-byte hexadecimal SHA-256 checksum, null-padded (`\x00`).

    ```
    |------------------|-----------------|
    | File Name (64)   | Content Hash (64)|
    |------------------|-----------------|
    ```
3.  **Server Response (ACK):** Server verifies the content hash matches the namespace mapping, then deletes the mapping on BoltDB and responds with a 1-byte status code (`'0'` for success, `'1'` for error/unauthorized).

### Native File Retrieval (GET - `'G'`)

When `RequestedMode` is `ModeGet` (`'G'`), the client requests the raw binary payload download of a specific file. The client must provide a proof-of-knowledge (content hash) to prevent unauthorized file discovery by name alone (CVE-009).

1.  **Handshake:** Completed with `'G'`.
2.  **Target Name + Hash Proof (Client sends):** Client sends a 128-byte request containing:
    -   **File Name:** 64-byte ASCII string, null-padded (`\x00`).
    -   **Content Hash:** 64-byte hexadecimal SHA-256 checksum, null-padded (`\x00`).

    ```
    |------------------|-----------------|
    | File Name (64)   | Content Hash (64)|
    |------------------|-----------------|
    ```
3.  **Server Response (ACK/Payload):**
    -   If the content hash does not match the namespace mapping for the given name, the server writes a 1-byte `'1'` (Unauthorized/Not Found) code and closes.
    -   If the file does not exist, the server writes a 1-byte `'1'` (Not Found) code and closes.
    -   If a server error occurs, the server writes a 1-byte `'2'` (Server Error) code and closes.
    -   If the file exists and the hash matches, the server writes a 1-byte `'0'` (Success) code, followed by a 64-byte null-padded `FileSize` string, followed by the raw binary stream of the file until EOF.

### Threshold-OPRF Evaluation (OPRFEval - `'O'`)

When confidential dedup is enabled (`oprf_enabled`), the client calls this native mode to derive a content key from a dedup tag without revealing the tag to any server. It is used before upload (to encrypt with the derived key) and before download (to decrypt with it).

1.  **Handshake:** Completed with `'O'` (auth token or challenge-response as configured).
2.  **Blinded Tag (Client sends):** The client computes `tag = SHA-256(plaintext)`, hashes it into the Ristretto255 group, blinds it with a random scalar `r`, and sends the 32-byte encoded blinded element. The client keeps `r` secret.
3.  **Evaluation:** The server (the daemon the client dialed) evaluates the blinded point with its own Shamir share, then gathers peer evaluations over the P2P transport. It returns evaluations without ever unblinding — it sees only `r * H(tag)`, never `tag` or the derived key.
4.  **Server Response:**
    -   **Evaluation Count:** 4-byte big-endian count `N`.
    -   **Per-evaluation records:** `N` records, each `[4-byte ShareIndex] + [4-byte EvalLen] + [EvalLen bytes Eval]`.
    -   If the quorum (< `oprf_threshold` distinct evaluations) is not met, the server responds with count `0` and the client **fails closed** (the upload/download aborts; there is no convergent fallback).
5.  **Client Combine/Unblind:** The client interpolates the Shamir secret at `f(0)` over at least `oprf_threshold` distinct share evaluations, unblinds with `1/r`, and derives the content key = `SHA-256(OPRF output)`. Identical plaintexts always yield the same key across tenants and clients.

The CAS/dedup key remains `H(plaintext)`, so identical blobs deduplicate cluster-wide.

### Payload

The file payload is streamed until EOF. The server reads exactly the number of bytes specified in the `fileSize` metadata field.

### Limits

- **MaxFileSize:** 1 GB (`1024 * 1024 * 1024` bytes). Files exceeding this limit are rejected with `EBADMSG` at `server.go:283`.
- **MaxPathLength:** 4096 bytes. Virtual paths exceeding this limit are rejected with `EBADMSG` in `DecodeFileMetadataList`.
- **FileInfoLength:** 64 bytes. Hash and name fields exceeding this length are rejected with `EBADMSG` in `DecodeFileMetadataList`.

### Namespace Keying

The namespace bucket is keyed by the **full virtual path** (e.g., `dirA/file.txt`), not the base filename. This means `dirA/file.txt` and `dirB/file.txt` are distinct namespace entries pointing to potentially different content hashes. The full virtual path is sent over the wire as the `fileName` field (constructed by the client as `remotePath + "/" + baseName`). The server uses this full path directly as the storage key, preventing namespace collisions between files with the same base name in different virtual directories.

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
-   **Timeouts:** Connections are protected by rolling idle timeouts (30s, TCP only) and phased absolute deadlines (10s for handshake, 60s for metadata) to prevent Slowloris attacks.
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

### Heartbeat Payload (MsgHeartbeat)

```
[4 bytes: peer count] [for each peer: [4 bytes: peer ID] [2 bytes: addr len] [N bytes: addr]]
```

- **Peer count**: Number of peers in the heartbeat (max `MaxPeersInHeartbeat=256`)
- **Per peer**: Peer ID (int32) + address string length + address bytes

### Query Payload (MsgQuery)

```
[1 byte: query type] [8 bytes: request ID] [N bytes: data]
```

- **QueryType**: `QueryList=1`, `QueryGet=2`, `QueryHas=3`, `QueryDelete=4`
- **RequestID**: Unique ID for matching responses to requests
- **Data**: Query-specific data (e.g., file name for QueryGet)

### Query Response Payload (MsgQueryResponse)

```
[8 bytes: request ID] [4 bytes: data len] [N bytes: data] [2 bytes: err len] [M bytes: err]
```

- **RequestID**: Matches the originating query
- **Data**: Response data (e.g., file list for QueryList)
- **Error**: Error string (empty if successful)

### Lease Payload (MsgLeaseRequest / MsgLeaseGrant / MsgLeaseRelease)

```
[8 bytes: lease ID] [4 bytes: key len] [N bytes: key] [8 bytes: expiry unixnano]
```

- **LeaseID**: Unique lease identifier
- **Key**: Resource key being leased
- **Expiry**: Lease expiration timestamp (unix nano)

## E2EE Content Encryption (Phase 3)

When `encryption_enabled = true` and a valid `encryption_key` (64-char hex, 256-bit) is configured, the client applies end-to-end encryption before sending any content or metadata to the server. The server remains **zero-knowledge** — it stores ciphertext and opaque metadata without ever seeing plaintext.

### Content Encryption

1. **Client reads the file** into memory.
2. **Encrypts with AES-GCM-256** (`crypto.Encrypt`): random 12-byte nonce prepended to ciphertext + 16-byte auth tag.
3. **Computes SHA-256 of ciphertext** → this is the content hash used for CAS dedup and CRUSH placement.
4. **Sends metadata** with the ciphertext hash and ciphertext size.
5. **Writes ciphertext** to the communicator (replaces `io.Copy` with direct write of encrypted bytes).

### Metadata Encryption (Filename Obfuscation)

The `wireName` (virtual path + filename) is obfuscated using **HMAC-SHA256** with the tenant-derived key:

```
encryptedName = hex(HMAC-SHA256(tenantKey, wireName))
```

This produces a deterministic 64-character hex string that:
- Fits within the `FileInfoLength` (64) limit
- Is opaque to the server (no plaintext filename stored)
- Is deterministic (same file → same encrypted name, enabling re-download)

**Limitation**: HMAC is one-way, so LIST responses return opaque hashes. The client can match known files by recomputing `HMAC(tenantKey, knownName)` for each file it knows about.

### Per-Tenant Key Isolation

The master encryption key is never used directly for content encryption. Instead, a **tenant-specific key** is derived using HKDF-SHA256:

```
tenantKey = HKDF-SHA256(masterKey, salt=nil, info=tenantID)
```

Different tenants produce different derived keys, ensuring content encrypted by one tenant cannot be decrypted by another.

### Download (GET) with Decryption

The `Download` function:
1. Connects to the server and sends a GET request with the encrypted name + ciphertext hash.
2. Reads the ciphertext from the server.
3. Decrypts with AES-GCM-256 (`cipher.Decrypt`).
4. Writes plaintext to the destination.

### Replication (ConnectStream)

Server-to-server replication forwards **ciphertext** as-is (passthrough). The server does not have the encryption key and cannot decrypt. Replicated copies remain encrypted.

### Server-Side (Zero Knowledge)

No changes to `src/server/` or `src/storage/` are required for E2EE:
- The server's `TeeReader` hashes the wire bytes (ciphertext), producing `SHA-256(ciphertext)` which matches the client-provided hash.
- The content-addressable store keys on the ciphertext hash, enabling dedup.
- The namespace stores the HMAC-encrypted filename, not the plaintext.
- The server never sees plaintext content or filenames.

## Server-Side Encryption at Rest (SSE)

When `encryption_enabled = true`, the server wraps its `BlobStore` with
`EncryptedBlobStore` — a decorator that encrypts blob content with
AES-GCM-256 before writing to the underlying storage backend (local, S3,
or raw device). This provides defense-in-depth: even if an attacker gains
access to the storage medium, blob content remains encrypted at rest.

### Architecture

```
Client → [E2EE encryption] → Server → EncryptedBlobStore → Underlying BlobStore
                                         (AES-GCM-256)         (local/S3/raw)
```

- **Decorator pattern:** `EncryptedBlobStore` implements the `BlobStore` interface, wrapping any underlying implementation.
- **Streaming AEAD:** `PutBlob` uses `EncryptStream` (4KB chunks); `GetBlob` uses `DecryptStream` via `io.Pipe` for zero-copy streaming.
- **Dedup preserved:** The hash key remains the plaintext content hash (computed by `CASStore` before calling `PutBlob`), so CAS dedup works on plaintext content.
- **Delete passthrough:** `DeleteBlob` delegates directly to the underlying store — encryption does not affect deletion semantics.
- **S3 metadata (filenames) remain plaintext** — only blob content is encrypted at rest.

### Configuration

```ini
[global]
encryption_enabled = true
encryption_key = <64-char hex string (32 bytes)>
encryption_tenant = default
```

When `encryption_enabled = true` and `encryption_key` is set, the storage
factory (`NewStore`) wraps the blob store with `EncryptedBlobStore` before
passing it to `CASStore`. No changes to S3 key handling or S3 communicator
logic are required.
