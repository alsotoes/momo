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

### S3 inbound gateway TLS requirements (issue #775)

The `s3-tcp` protocol **requires** TLS. When no `tls_cert`/`tls_key` are configured, `Listen` returns an `EINVAL` error unless `tls_insecure = true` is explicitly set (in which case a prominent startup warning is emitted and the gateway serves S3 over cleartext HTTP). This mirrors the AWS standard of TLS-only S3.

The `momo-tcp` protocol is unaffected — it carries its own authentication via the momo handshake and does not require TLS.

QUIC protocols (`s3-quic`, `momo-quic`) always use TLS 1.3 via QUIC. When no `tls_cert`/`tls_key` are configured, a self-signed certificate is generated and a warning is logged noting that the connection is encrypted but the server identity is unauthenticated.

When TLS is enabled, the momo handshake uses **challenge-response authentication** instead of sending the auth token in plaintext.

Servers may optionally throttle challenge-response failures via `auth_backoff_delay`
(see `CONFIGURATION.md`): a source that repeatedly fails authentication is met
with adaptive exponential backoff and, past a threshold, a temporary lockout.
This is a **server-side admission-policy only** — it adds no bytes to the wire
handshake (nonce and response sizes are unchanged), so the protocol framing is
bit-for-bit stable regardless of the setting (issue #821).

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

### Threshold-OPRF Evaluation over S3 (`s3-tcp` / `s3-quic`)

OPRF is inherently a **momo-native, client-driven** handshake (raw binary `'O'` mode above). A momo client that enables `oprf_enabled` uses the native transports for OPRF evaluation. The S3 gateway existed to serve standard S3-ecosystem clients (aws-cli, SDKs), which cannot perform OPRF; therefore OPRF-on-S3 is **not a designed parity surface**. For completeness, `S3Communicator` also exposes an RPC mirror of the evaluation over a dedicated HTTP endpoint, which is available but has no designed consumer:

```
POST /?momo-oprf-eval HTTP/1.1
Authorization: Bearer <authToken>   (or SigV4)
X-Momo-Timestamp: <unix nano>       (optional)
Content-Length: 32                  (always 32)

<32-byte blinded Ristretto255 element>
```

Response `200 OK` body carries the same evaluation wire layout as the native mode — `[4-byte BE count N]` then `N × [4-byte BE ShareIndex + 4-byte BE EvalLen + EvalLen bytes]` — so the client decoder is shared. Errors: `400 InvalidRequest` for a non-32-byte blinded tag, `501 NotImplemented` when OPRF is not enabled on the node, `500 InternalError` if the server-side evaluation fails. The endpoint is authenticated like every S3 request; unauthenticated callers are rejected before the handler runs. On quorum failure the server returns count `0` and the client fails closed (`EAGAIN`), matching the native transports.

The CAS/dedup key remains `H(plaintext)`, so identical blobs deduplicate cluster-wide.

### Payload

The file payload is streamed until EOF. The server reads exactly the number of bytes specified in the `fileSize` metadata field.

### Limits

- **MaxFileSize:** 1 GB (`1024 * 1024 * 1024` bytes). The server rejects oversized/negative `fileSize` values (checked at `server.go:432`), logs an audit line, and closes the connection. The failure maps to `EBADMSG` for POSIX compliance when surfaced as an error, but is not transmitted as a wire error — the client sees the connection close.
- **MaxPathLength:** 4096 bytes. Virtual paths exceeding this limit are rejected with `EBADMSG` in `DecodeFileMetadataList`.
- **FileInfoLength:** 64 bytes. Hash and name fields exceeding this length are rejected with `EBADMSG` in `DecodeFileMetadataList`.
- **Fixed-length padding:** Wire fields padded via `common.PadString` (auth tokens, timestamps, hashes, names) must never exceed their fixed byte width. `PadString` **panics** on overlong input instead of silently truncating (recovered at caller boundaries per Rule 37), so an overlong value that slips past validation surfaces as a `syscall.EIO` failure rather than corrupting the stream.

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
3.  **REST Query Routing:** Because the request method is `GET`, `HEAD`, or `DELETE`, S3Communicator detects it as a REST query and **bypasses standard Momo framing**:
    -   **ListBuckets:** `GET /` queries the configured single bucket (from the `[storage] s3_bucket` setting) and returns an S3-compliant `<ListAllMyBucketsResult>`. With no bucket configured (legacy flat mode), it returns an empty bucket list.
    -   **HeadBucket / HeadObject:** `HEAD /bucket` returns `200 OK` (or `404 NoSuchBucket` outside the configured bucket); `HEAD /bucket/key` returns headers-only `200 OK` with `ETag`, `Content-Length`, `Last-Modified`, the preserved `Content-Type`, and `x-amz-meta-*` user metadata, or `404 NoSuchKey`.
    -   **CreateBucket / DeleteBucket:** `PUT /bucket` returns `200 OK` with a `<LocationConstraint>` body for the configured bucket (bucket semantics require `s3_bucket` to be set); `DELETE /bucket` returns `204 No Content` when the store is empty or `409 BucketNotEmpty` otherwise.
    -   **GetBucketLocation:** `GET /bucket?location` returns `200 OK` with a `<LocationConstraint/>` body.
    -   **ListObjectsV2:** Queries `store.List()`, supports `prefix`, `delimiter`, `max-keys`, `continuation-token`/`start-after` and `fetch-owner` parameters, formats the page into S3-compliant XML using a high-performance allocation-free `bytes.Buffer`, and emits `KeyCount`/`IsTruncated`/`NextContinuationToken` per AWS semantics. Writes `200 OK` back to the client and returns `ErrRequestHandled`.
    -   **GetObject:** Queries `store.Get(key)`, evaluates conditional headers (`If-Match`/`If-None-Match`→`304`/`412`, `If-Modified-Since`, `If-Range`) and the `Range` header (`206 Partial Content` with `Content-Range`, `416 Range Not Satisfiable` with `bytes */size`), streams the binary content (or the requested span) directly to the client with the object `ETag`/`Last-Modified` and the preserved `Content-Type`/`x-amz-meta-*` headers, and returns `ErrRequestHandled`.
    -   **Object Metadata Preservation (PUT/GET/HEAD):** `Content-Type`, `Cache-Control`, `Content-Disposition`, `Content-Encoding`, `Expires`, `x-amz-server-side-encryption` (accepted value `AES256` only), and every `x-amz-meta-*` user header sent on an S3 `PUT` are validated (values capped at 1024 bytes, CR/LF stripped), persisted at rest by `CASStore.PutS3Meta` (bounded to 8192 bytes of JSON), echoed on the corresponding `GET`/`HEAD`/`Range`/`304` responses (sorted, `application/octet-stream` default when absent), and propagated to replicated peers through an additive `X-Momo-S3-Meta` header (base64-encoded JSON) in the S3 peer `PUT` framing. The additive flow does not alter the fixed Momo wire fields (`Name`/`Hash`/`Size`/`RemotePath`), and store/peer implementations without support transparently ignore the metadata.
    -   **DeleteObject:** Invokes `store.Delete(key)` on BoltDB, writes a `204 No Content` response, and returns `ErrRequestHandled`.
    -   **CopyObject:** `PUT /bucket/key` with an `x-amz-copy-source` header copies an existing object within the store via `store.Get` → `store.Put` (creating a content-addressed namespace alias, preserving the source's S3 metadata), returns a `<CopyObjectResult>` XML body (`ETag`, `LastModified`), and returns `ErrRequestHandled`.
    -   **Multipart Upload (issue #764):** All six multipart endpoints are intercepted end-to-end (bypass momo framing, return `ErrRequestHandled`). Upload state is tracked in memory (`map[string]*multipartUpload` with `sync.Mutex`). On `CompleteMultipartUpload`, parts are sorted by part number, assembled in order, hashed with SHA-256 (the hash becomes the ETag), and stored via `store.Put()` so the assembled object flows through the normal momo CAS/persistence/replication path:
        - `POST /bucket/key?uploads` -> `CreateMultipartUpload`: returns `InitiateMultipartUploadResult` XML with `UploadId`.
        - `PUT /bucket/key?uploadId=X&partNumber=N` -> `UploadPart`: stores the part body (SHA-256 ETag), returns `200 OK` with `ETag` header.
        - `POST /bucket/key?uploadId=X` -> `CompleteMultipartUpload`: assembles parts, computes final hash, stores via `store.Put()`, returns `CompleteMultipartUploadResult` XML.
        - `DELETE /bucket/key?uploadId=X` -> `AbortMultipartUpload`: cleans up tracked state, returns `204 No Content`.
        - `GET /bucket/key?uploadId=X` -> `ListParts`: returns S3 `ListPartsResult` XML.
        - `GET /?uploads` or `GET /bucket?uploads` -> `ListMultipartUploads`: returns S3 `ListMultipartUploadsResult` XML.
    -   **DeleteObjects (batch):** `POST /bucket?delete` reads a bounded `<Delete>` XML payload (≤1000 keys, ≤1MB body), routes each key through the single-DELETE path (lease → `store.Delete` → scatter-gather delete propagation), aggregates the per-key results into `<DeleteResult>` XML (`Deleted`/`Error` entries, `200 OK` even when keys are missing), and returns `ErrRequestHandled` — no momo framing is emitted for the batch itself.
    -   **Unsupported subresource → `501 NotImplemented`:** if a request carries a known-but-unsupported subresource query param, `S3Communicator` intercepts it at the dispatch root (before any store/method routing) and returns a clean `501` with the `NotImplemented` code and a bounded write, store-independent. Bucket-config set (`key == ""`, issues #912/#913/#920): `?versioning`, `?versions`, `?acl`, `?policy`, `?cors`, `?website`, `?lifecycle`, `?tagging`, `?encryption`, `?publicAccessBlock`, `?accelerate`, `?replication`, `?requestPayment`, `?logging`, `?object-lock`, `?notification`, `?analytics`, `?inventory`, `?metrics`, `?intelligent-tiering`. Object-level set (`key != ""`, issues #914/#915/#920): `?tagging`, `?acl`, `?versionId`, `?retention`, `?legal-hold`, `?select`. `UploadPartCopy` (`PUT ?uploadId&partNumber` + `X-Amz-Copy-Source`, issue #920) is intercepted in the PUT dispatch before the UploadPart handler. Supported subresources (`?location`, `list-type` + pagination, `uploads`, `uploadId`/`partNumber`, batch `?delete`) are unaffected.
4.  **Graceful Termination:** Upon receiving the `ErrRequestHandled` sentinel error from the handshake, the server daemon disables Momo replication acknowledgements (ACKs) and immediately closes the connection gracefully. The S3 client receives standard HTTP bytes and never sees custom Momo handshakes.

**Presigned URL (query-string SigV4) authentication:** `HandshakeServer` accepts both the `Authorization: AWS4-HMAC-SHA256 Credential=...` header form and the *presigned* form, where the SigV4 parameters travel in the URL query string (`X-Amz-Algorithm=AWS4-HMAC-SHA256`, `X-Amz-Credential=AccessKey/date/region/s3/aws4_request`, `X-Amz-Date`, `X-Amz-Expires`, `X-Amz-SignedHeaders`, `X-Amz-Signature`). The access key is extracted from the `X-Amz-Credential` scope, the signature is verified over the canonical request (with `X-Amz-Signature` excluded from the canonical query string), and the request is rejected once `X-Amz-Date + X-Amz-Expires` passes. Payload hashing follows S3 presign semantics: `UNSIGNED-PAYLOAD` for `PUT` uploads and the empty-body SHA256 (`e3b0c442...b855`) for `GET`/`HEAD`/`DELETE`. Failures map to S3 errors: `403 SignatureDoesNotMatch` for a bad signature, `403 AccessDenied (Request has expired)` for an expired URL, and `400 AuthorizationQueryParametersError` when `X-Amz-Algorithm` is present but required parameters are missing. This enables issuing time-limited, capability-based URLs (e.g., `aws s3 presign`) without exposing the token.

**aws-chunked streaming uploads (issue #773):** The gateway de-frames S3's streaming body format at the transport boundary so stored blobs and momo content addressing use the *decoded* payload. When a `PUT` carries `X-Amz-Content-Sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD` (or the `-TRAILER` variants) or `Content-Encoding: aws-chunked`, the request's SigV4 verification uses the `STREAMING-*` literal as the canonical payload hash (per the AWS spec), and the body is then parsed as `<hex-size>[;chunk-signature=<64hex>]\r\n<raw bytes>\r\n...` chunks ending in a `0[;chunk-signature=...]\r\n\r\n` terminating chunk (plus an optional trailing-header block on trailer variants). Each chunk's signature is verified with the chained SigV4 streaming string-to-sign (`AWS4-HMAC-SHA256-PAYLOAD\ndate\nscope\nprevious-signature\ne3b0c442...b855\nhex(sha256(chunk-data))`) keyed by the derived signing key and seeded with the request signature; a mismatch maps to `403 SignatureDoesNotMatch`. The de-framed bytes are spilled to a bounded temp file (disk, `MaxFileSize`), `meta.Hash`/`meta.Size` are resolved from the decoded content hash and size (cross-checked against `X-Amz-Decoded-Content-Length`), and `S3Communicator.Read()` replays the spill so the standard momo pipeline sees only de-framed content. The gateway emits `HTTP/1.1 100 Continue` when the client sends `Expect: 100-continue`, and body reads are bounded by a size-proportional deadline. `STREAMING-UNSIGNED-PAYLOAD-TRAILER` and `aws-chunked` bodies without a signing context are de-framed without per-chunk verification (the documented S3 unsigned posture). Chunk header lines, chunk sizes, and trailer blocks are all length-bounded to prevent resource-exhaustion from a malicious peer. The storage backend (`S3BlobStore`) signs its own client uploads with `SIGNED_PAYLOAD` (issue #776): the body is spooled to a bounded temp file while its SHA-256 is computed, uploaded with a real `Content-Length`, and `X-Amz-Content-Sha256` is set to the content hash so the SigV4 signature binds the body — a tampered body fails signature verification at the endpoint. Oversized blobs are rejected (`EFBIG`) before any upload.

**SSE negotiation (issue #776):** The gateway never silently downgrades a server-side-encryption request. `x-amz-server-side-encryption: AES256` on a `PUT` is honored: the header is captured as S3 object metadata, persisted at rest, and echoed on `GET`/`HEAD` (momo encrypts objects at rest with its own AES-256-GCM envelope). SSE-C customer-key headers (`x-amz-server-side-encryption-customer-algorithm`/`-key`/`-key-md5`) are rejected with `400 InvalidRequest` and the customer key is never stored; SSE-KMS (`aws:kms` or `x-amz-server-side-encryption-aws-kms-key-id`) is rejected with `501 NotImplemented`; any other algorithm is rejected with `400 InvalidArgument`. The validation runs for every PUT variant (object upload, CopyObject, CreateBucket). `x-amz-sdk-checksum-algorithm` (sent by default by aws-cli v2) is accepted without rejection; momo does not compute AWS additive checksums — object integrity is content-addressed SHA-256 plus AEAD at rest. Inbound `UNSIGNED-PAYLOAD` presigned uploads and unsigned streaming bodies continue to be accepted (see presigned and aws-chunked sections).

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
| `MsgOPRFEvalRequest` | 12 | OPRF evaluation request (confidential dedup) |
| `MsgOPRFEvalResponse` | 13 | OPRF evaluation response |

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
- **Streaming AEAD:** `PutBlob` uses `EncryptStream` (default 4KB chunk size, v4 format); `GetBlob` uses `DecryptStream` via `io.Pipe` for zero-copy streaming.
- **Dedup preserved:** The hash key remains the plaintext content hash (computed by `CASStore` before calling `PutBlob`), so CAS dedup works on plaintext content.
- **Delete passthrough:** `DeleteBlob` delegates directly to the underlying store — encryption does not affect deletion semantics.
- **S3 metadata (filenames) remain plaintext** — only blob content is encrypted at rest.

### Outbound TLS to the S3 storage backend (issue #774)

The `S3BlobStore` (outbound client to a real S3/MinIO endpoint) enforces TLS on the storage endpoint. In `NewS3BlobStore` the `s3_endpoint` scheme is validated before any request is issued:

- `https://` — always accepted (default posture).
- `http://` — **rejected with an `EINVAL` config error unless `s3_insecure = true`** is explicitly set, in which case a prominent warning is logged at startup. This prevents silent cleartext transmission of SigV4 credentials, blob payloads, and object metadata.
- Missing or unsupported schemes (e.g. `ftp://`) are rejected with `EINVAL`.

This enforcement is orthogonal to the inbound gateway framing (s3-tcp/s3-quic/momo-tcp/momo-quic): the `S3BlobStore` sits below the storage layer, so behavior is identical regardless of which inbound protocol served the data. TLS here protects the wire in *addition to* the AES-GCM-256 at-rest ciphertext.

### Layered confidentiality model

The "real E2EE boundary" is a stack of independent protections:

1. **Inbound gateway TLS** — TLS 1.3 for QUIC protocols (s3-quic/momo-quic); TLS 1.2+ for TCP protocols (s3-tcp/momo-tcp) when `tls_cert`/`tls_key` are configured (see [Transport TLS](#transport-tls-phase-1--e2ee)); s3-tcp refuses cleartext S3 serving without an explicit insecure override.
2. **Client-side E2EE** — when `encryption_enabled = true`, the client encrypts content before it ever reaches the server.
3. **At-rest encryption** — `EncryptedBlobStore` AES-GCM-256 wraps whatever the underlying backend is.
4. **Outbound storage TLS** — when the backend is S3, `S3BlobStore` requires HTTPS (or an explicit `s3_insecure` override) so replication to the S3 endpoint is never silently downgraded to cleartext.

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

## Streaming AEAD Wire Format

Both Phase 3 client-side E2EE and the SSE `EncryptedBlobStore` use the same streaming AEAD format via `EncryptStream` / `DecryptStream`. This format provides chunk-bound authenticated encryption with a cryptographic integrity footer that prevents undetected truncation.

### Stream Layout

```
[1B version][2B chunkSize][8B seed][4B sealedLen][N sealed chunk] ... [4B sealedLen][M sealed footer]
```

| Field | Size | Description |
|-------|------|-------------|
| **version** | 1 byte | `0x04` (current v4); `0x03` (legacy v3); `0x02` (legacy v2, no integrity footer) |
| **chunkSize** | 2 bytes | Big-endian plaintext chunk length, `[MinChunkSize, MaxChunkSize]` = `[512, 4096]` (v4 only) |
| **seed** | 8 bytes | Random per-stream seed, never reused with the same key |
| **sealedLen** | 4 bytes | Big-endian uint32 length of the sealed chunk/footer body (max `MaxChunkSize` = 4128) |
| **sealed chunk** | sealedLen bytes | AEAD-sealed (AES-256-GCM) data chunk |
| **sealed footer** | sealedLen bytes | AEAD-sealed integrity footer (v3/v4 only) |

The stream is a sequence of zero or more data chunks followed by exactly one footer chunk. The footer is mandatory in v3 and v4; v2 (the prior format) stops after the last data chunk with no footer and is decoded for backward compatibility. The v4 header carries a **self-describing chunk size** so a decoder can validate the bound before allocating (Rule 32) and read streams written with a non-default chunk size; `SetStreamChunkSize` raises/lowers this within `[512, 4096]` (issue #824).

### Data Chunk

Each data chunk encrypts up to the stream's declared `chunkSize` bytes of plaintext (default `4096`):

```
nonce  = seed[0:8] || big-endian(chunkIndex)[8:12]   (12 bytes)
sealed = AES-256-GCM.Seal(nonce, plaintext, aad=nil)  (plaintext + 16B tag)
```

- **chunkIndex** starts at 0 and increments by 1 for each data chunk.
- The first 8 bytes of the nonce come from the stream seed. The last 4 bytes are the big-endian chunk counter, guaranteeing a unique (key, nonce) pair across every chunk in the stream.
- Chunk AAD is `nil`. Only the footer uses domain-separated AAD.

### Integrity Footer (v3/v4)

The footer is a single AEAD-authenticated record appended after the last data chunk:

```
nonce  = seed[0:8] || 0xFFFFFFFF
aad    = "momo:stream-footer:v1"
footer = AES-256-GCM.Seal(nonce, big-endian(chunkCount)[0:4], aad)
```

- **chunkCount**: the exact number of data chunks in the stream (4 bytes, big-endian).
- The nonce index `0xFFFFFFFF` is reserved for the footer and will never collide with a data chunk index.
- The AAD `"momo:stream-footer:v1"` is domain-separated from data chunks. If an attacker tries to reorder a footer as a data chunk (or vice versa), the AEAD authentication fails.
- **On decode (v3/v4)**: `DecryptStream` first tries data-chunk AAD (`nil`). If that fails, it retries with the footer AAD and nonce. If both fail, the stream is declared tampered (`ErrTampered`). If a footer authenticates but `chunkCount` does not match the actual decoded chunk count, the stream is rejected (`EBADMSG`). If the stream ends before a footer is found, it is rejected as truncated (`EIO`).

### Wire Format Diagram (v4)

```
         +--------+----------+--------+--------+---------+---+--------+----------+
Stream:  | 0x04   | chunkSize| seed   | len(0) | chunk(0)|...| len(F) | footer   |
         +--------+----------+--------+--------+---------+---+--------+----------+
Byte:    0        1          3        11       15          N         N+4        N+4+len(F)
         version 2BE size   8B rand  4BE len  ciphertext         4BE len   sealed(4B + 16B tag)
```

## Envelope E2EE (Phase 4, issue #780)

The `encryption_key` model above is **shared secret**: both the client and the
server hold the same master key, so the server could decrypt content in
principle. For **zero-trust vs. the serving node**, Momo supports a client-held
envelope model (all 4 protocols: `momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`)
modeled on the AWS S3 Encryption Client v3:

- A **client-held** `e2ee_key` (64-hex, 256-bit) is configured client-side
  only; it is **never** configured on, or sent to, any server daemon.
- On upload, the client generates a fresh **per-object data key**, wraps it
  with the client-held master key, and writes a self-describing envelope header
  (`MOMOENV1` magic + version/algorithm + key-id + wrapped data key) followed by
  the streaming AES-256-GCM ciphertext under the data key
  (`crypto.EncryptEnvelope`).
- The server stores the envelope bytes **as-is** (opaque). Its `TeeReader`
  hashes the wire bytes → `SHA-256(envelope)`, which becomes the CAS/dedup key.
  The server can neither read nor derive the content key; SSE
  (`EncryptedBlobStore`) may still wrap the opaque envelope for at-rest
  defense-in-depth, but that is optional.
- On download, the client reads the envelope, unwraps the data key with its
  client-held master key, and streams the plaintext out
  (`crypto.DecryptEnvelope`).
- Metadata (filename) confidentiality uses the same HMAC-SHA256 obfuscation as
  Phase 3, but the HMAC key is derived from the **client-held** master key
  (`DomainContent`), so the server can never derive it either.
- **Mutually exclusive with OPRF** confidential dedup in this iteration: the
  two models share the wire format's key-management slot. Config with both
  `e2ee_key` and `oprf_enabled` is rejected.

```
Client → [EnvelopeE2EE: wrap data key + EncryptStream] → Server → [opaque store]
```

### Configuration (client side)

```ini
[global]
e2ee_key = <64-char hex string (32 bytes)>
e2ee_key_id = default
```

Or via CLI flag for the native client: `momo -imp client -file F -e2ee-key <hex>`.
The same `-e2ee-key` / `-e2ee-key-id` flags power the S3 `s3enc` / `s3dec`
impersonations (issue #777).
