# Momo Error Codes and Exit Status

This document provides a reference for the error codes and exit statuses that the Momo application may produce. Understanding these codes is crucial for debugging and operating a Momo cluster effectively.

## Overview

Momo relies on standard POSIX system calls for network and file I/O. As such, many of the errors it encounters are standard Unix error codes. The application will typically print an error message to `stderr` that includes the specific error code and then exit with a non-zero status.

The general exit code for many fatal errors is `1`.

## Common System Call Errors

Below is a list of common `errno` values that might be encountered and their specific meaning in the context of Momo.

| Error Code | Constant | POSIX Meaning | Momo Context & Interpretation |
| :--- | :--- | :--- | :--- |
| `EBADMSG` | Bad message | The message received does not conform to the protocol. | This often points to data corruption during transit or an issue with how the message was framed (e.g., incorrect metadata size). The SHA-256 hash validation failure triggers this error (returned as part of the error chain). Also returned by `DecodeFileMetadataList` for path traversal chars or oversized length fields (Rule 32). |
| `EACCES` | Permission denied | An attempt was made to access a file in a way forbidden by its file access permissions. | In Momo, this is used to indicate an authentication failure when an invalid AuthToken is provided during the handshake. |
| `ETIMEDOUT`| Connection timed out | A connection attempt timed out, or a connected partner has not responded. | Could occur during the initial handshake if the server is unresponsive, or during the file transfer if there is a network partition or a server becomes overloaded and cannot respond. |
| `ECONNREFUSED`| Connection refused | The target machine actively refused the connection. | This is a straightforward network error. It means a Momo server is not running or is not reachable at the specified IP address and port, or a firewall is blocking the connection. |
| `EPIPE` | Broken pipe | An attempt was made to write to a pipe or socket that is not open for reading on the other end. | This commonly occurs if the client or a downstream server closes the connection while an upstream server is still trying to send data. |
| `EIO` | I/O error | A physical I/O error has occurred. | This indicates a problem with the underlying storage hardware on one of the servers, or a serious data integrity issue. |
| `ENOSPC` | No space left on device | The filesystem has no free blocks. | Preserved across the whole write path (R7, #935): `LocalBlobStore.checkDiskSpace` probes `Statfs` before writing and returns `ENOSPC` when there are no free blocks or less than 1 MiB of headroom, and the original `ENOSPC` survives `MkdirAll`/`CreateTemp`/`io.Copy`/`Sync`/`Rename` instead of being masked as `EIO`. `CASStore.Put` propagates it; the S3 gateway maps it to `507 InsufficientStorage` (CopyObject and CompleteMultipartUpload) and native transports write an explicit error status before closing. |
| `ENOENT` | No such file or directory | The specified file or directory does not exist. | In the Momo Object Store, this signifies that a requested human-readable name or content hash was not found in the local Bbolt metadata index. |
| `E2BIG` | Argument list too long | The number of peers in a heartbeat exceeds the maximum. | P2P gossip truncates heartbeats to `MaxPeersInHeartbeat=256` peers. If a heartbeat exceeds this, the excess peers are dropped and `E2BIG` is logged. |
| `EFBIG` | File too large | A P2P payload exceeds the maximum allowed size. | P2P RPC payloads are capped at `maxPayloadSize=1 MiB`. Payloads exceeding this limit are rejected with `EFBIG` to prevent memory exhaustion from malicious peers. |
| `ECANCELED` | Operation canceled | An operation was canceled because the system is shutting down. | The P2P lease manager returns `ECANCELED` when a lease operation is attempted after `Stop()` has been called. |
| `EHOSTUNREACH` | Host unreachable | The remote host is unreachable. | P2P gossip and lease operations return `EHOSTUNREACH` when an RPC send fails (peer is down or network partition). Used in `sendPing`, `sendIndirectPing`, lease acquisition, and scatter-gather queries. |
| `ENAMETOOLONG` | File name too long | The virtual file path exceeds the maximum length. | Returned by the storage layer and TCP/QUIC transports when a file name or virtual path exceeds the protocol's fixed-size field boundaries. |
| `EINVAL` | Invalid argument | An invalid argument was provided. | Returned by the config parser (e.g., empty `ReplicationOrder`), P2P payload decoder (invalid count), query handler (empty data), and CRUSH placement (invalid node count). |
| `ENOBUFS` | No buffer space available | A bounded read limit was exceeded. | The S3 gateway bounds the **request head** at 64 KiB (`LimitedConnReader.SetLimit(65536)` around `http.ReadRequest`) and returns `ENOBUFS` when a request line or header block exceeds it. The limit is cleared immediately after the head is parsed, so streaming bodies are never bounded (regression #653). |
| `EPROTO` | Protocol error | A protocol-level handshake failed. | Returned when a P2P peer handshake completes without the expected protocol reply (e.g. dialling a non-peer port), mapped from the transport error by the rebalance push path. |
| `ECONNABORTED` | Connection aborted | Software caused connection abort. | Returned by `src/common/net.go` when a connection is aborted by the host system. |
| `ECONNRESET` | Connection reset by peer | Connection reset by peer. | Returned by `src/p2p/tcp_transport.go` when a P2P peer disconnects or its reads error out (logged with `errno`). |
| `ENOTCONN` | Transport endpoint is not connected | Socket not connected. | Returned by `src/p2p/tcp_transport.go` when an RPC targets a peer that has no active connection. |
| `ENETDOWN` | Network is down | Network interface not available. | Returned when the replication server goroutine panics (`src/momo.go`). |
| `EADDRINUSE` | Address already in use | Port binding conflict. | Logged by `src/p2p/tcp_transport.go` when the P2P gossip listener fails to bind (port already in use). Not currently surfaced for the daemon's main server bind port. |

## Application-Specific Exit Codes

Fatal startup errors exit with a cause-specific status (`fatalExit` in
`src/momo.go`, R7 #935) so supervisors can distinguish a bad config from a full
disk without parsing logs.

| Exit Code | Meaning |
| :--- | :--- |
| `1` | **General fatal error** — anything without a more specific classification. |
| `2` | **Configuration error** — missing/invalid `momo.conf`, bad flags, daemon index out of range, unsafe feature combination rejected at load time (e.g. `[rebalance] enabled=true` without `degraded_read`). |
| `3` | **Network error** — listener bind failure or unreachable dependency at startup. |
| `4` | **Storage error** — store/database initialization failure, disk full (`ENOSPC`). |
| `5` | **Crypto error** — key decode/derivation failure, TLS material rejected. |
| `6` | **P2P error** — gossip/lease initialization failure. |

By cross-referencing the exit code with the error messages printed to standard
error, operators can quickly diagnose and resolve issues within the Momo cluster.

## S3 HTTP Error Mapping

The gateway translates internal errors into S3 codes; clients see a HTTP status,
an S3 `Code`, and a message.

| HTTP | S3 Code | Cause |
| :--- | :--- | :--- |
| `400` | `InvalidRequest` | Unsupported SSE request (SSE-C), conflicting checksum declaration, malformed XML, or a lock/retention field used on a bucket without the feature enabled. |
| `400` | `BadDigest` | Supplied `x-amz-checksum-*` did not match the received bytes (the object is not stored). |
| `400` | `InvalidArgument` | Unknown SSE algorithm or an out-of-range value. |
| `403` | `AuthorizationHeaderMalformed` | SigV4 header missing a required component, stale timestamp, or unsupported credential scope. |
| `403` | `AccessDenied` | Tenant authorization denied, governance retention weakened without `x-amz-bypass-governance-retention`, or a version/object protected by retention or legal hold. |
| `404` | `NoSuchKey` / `NoSuchVersion` | Unknown object or version. |
| `409` | `InvalidBucketState` | Illegal bucket-state transition (for example, enabling object lock with existing objects). |
| `409` | `BucketNotEmpty` | Bucket deletion attempted while objects remain. |
| `404` | `NoSuchUpload` / `NoSuchLifecycleConfiguration` / `NoSuchNotificationConfiguration` / `NoSuchObjectLockConfiguration` | The referenced multipart upload or bucket subresource configuration does not exist. |
| `503` | `ServiceUnavailable` | Lease acquisition failed (coordination quorum not reached). |
| `412` | `PreconditionFailed` | Conditional request (`If-Match`/`If-None-Match`) did not match. |
| `501` | `NotImplemented` | Known-unsupported subresource or operation (bucket configuration, object tagging/ACL, `SelectObjectContent`, `UploadPartCopy`, SSE-KMS). |
| `507` | `InsufficientStorage` | Local disk is full (`ENOSPC`). |

## Admin Endpoint Errors

`/metrics`, `/health/detailed` and `POST /reload-secrets` share the admin listener.

| HTTP | Cause |
| :--- | :--- |
| `401` | `[metrics] admin_token` is configured and the request lacks a matching `Authorization: Bearer <token>`. `/health` stays open so liveness probes need no credentials. |
| `403` | A mutating endpoint was called without an `admin_token` while the listener is bound off-loopback. Configure the token or bind to loopback; this is never allowed silently. |
