# Change: S3 Compatibility Layer
**Related Issues:**
- https://github.com/alsotoes/momo/issues/131
- https://github.com/alsotoes/momo/issues/133

## Why
As Momo moves towards cloud-native integration, providing an S3-compatible interface allows the cluster to interoperate with standard storage tools and SDKs. By implementing an S3 Protocol Handler, Momo can serve as a distributed, high-performance S3 gateway, utilizing its unique polymorphic replication modes (Chain, Splay) under the hood.

## Architectural Rationale: S3 as an Application Protocol
Following the decoupled architecture defined in Issue #131, S3 will be implemented as a `Communicator` plugin:
- **Transport Independence:** The S3 layer will work over both `TCP` (Standard HTTP) and `QUIC` (HTTP/3), leveraging the `ProtocolFactory`.
- **API Mapping:** S3 `PUT` and `GET` requests will be mapped to Momo's internal `SendFile` and `getFile` logic.
- **Replication Transparency:** The client interacts via S3, while the cluster maintains its metrics-driven replication behavior (e.g., an S3 upload to Node 0 might trigger a `Chain` replication to Node 1 and 2).

## What Changes
- Implement `S3Communicator` in `src/common` to handle S3-style headers and request parsing.
- Update the `ProtocolFactory` to support `s3-tcp` and `s3-quic` variants.
- Map S3 metadata (Bucket/Key) to Momo metadata (Name/Hash).
- Ensure the `AUDIT:` logging captures S3-specific operations for traceability.

## Impact
- Affected specs: `networking`, `storage`
- Affected code: `src/common/communicator`, `src/server/s3_handler`
