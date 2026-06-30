## Context

Momo is designed as a distributed content-addressable storage system with a metadata index managed via BoltDB on each node. While standard file storage operations over TCP and QUIC are supported, the existing S3 protocol gateway in Momo is limited to handling `PUT` file uploads. To achieve standard compatibility with modern cloud tools (like AWS SDKs and Ceph clients), the S3 protocol gateway must be enhanced with listing, deleting, and downloading capabilities, satisfying issue #225.

## Goals / Non-Goals

**Goals:**
- Provide full, S3-compliant listing (`ListObjectsV2`), deletion (`DeleteObject`), and file download (`GetObject`) APIs.
- Group virtual folders correctly via `Delimiter` and `Prefix` parameters (S3 simulated directory structures).
- Retain high-performance (zero memory allocations / **⚡ Bolt**) and secure (**🛡️ Sentinel**) code guidelines.
- Support both path-style and virtual-hosted addressing schemas.

**Non-Goals:**
- Multi-part upload endpoints (outside of basic PUT).
- S3 Bucket creation, deletion, or ACL management (since Momo operates with a global virtual namespace and direct file path structures).

## Decisions

### 1. Intercepting Requests in HandshakeServer
- **Choice:** Process `GET` and `DELETE` requests directly inside `S3Communicator.HandshakeServer`.
- **Rationale:** Momo's daemon flow assumes every transaction is a replication upload sequence (relying on `ReceiveMetadata` and receiving file payloads). For listing, retrieval, and deletion, we don't need the replication payload loops. By returning a new transport sentinel `ErrRequestHandled` from `HandshakeServer` after writing the HTTP response directly to the client connection, we bypass the daemon upload loop safely and elegantly, with zero architectural disruption.
- **Alternative:** Modifying the abstract `Communicator` interface and adding list/delete methods. This would require editing `MomoTCP` and `MomoQUIC` implementations as well as the daemon's core logic, introducing unwanted complexity.

### 2. URL Schema and Virtual Host Parsing
- **Choice:** Support both virtual-hosted (`bucket-name.s3.amazonaws.com`) and path-style (`/bucket-name/key`) parsing through a helper function `extractS3BucketAndKey`.
- **Rationale:** AWS S3 and Ceph use both styles depending on SDK settings and DNS structure. Supporting both ensures out-of-the-box compatibility with AWS CLI, boto3, and standard tools.

### 3. Allocation-free XML Response Formatting
- **Choice:** Manually build S3-compliant ListObjectsV2 XML response via `bytes.Buffer` and custom escape functions instead of using standard `encoding/xml` marshalling.
- **Rationale:** XML serialization via reflection is heavy on CPU and heap allocations. This manual formatting complies with the **⚡ Bolt** standard of using pre-allocated/stack-friendly structures, ensuring sub-millisecond list responses.

## Risks / Trade-offs

- **[Risk] Path Traversal Attacks:** A malicious S3 request could use `../` or `\` in the object key to access or delete arbitrary files outside of the store.
  - **Mitigation:** Rigorously run input path and key validation using the `hasPathTraversalChars` helper and `filepath.Clean` (**🛡️ Sentinel** pattern).
- **[Risk] Nil Store Reference Panic:** Invoking list/delete before the storage engine is fully loaded could cause a nil-pointer panic.
  - **Mitigation:** Explicitly verify `store != nil` in the communicator before invoking database actions.
