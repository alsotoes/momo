## Why

Momo currently operates as an S3 PUT-only gateway. To act as a complete, standard-compliant, and secure S3/Ceph object storage gateway, it must support listing objects, deleting objects, and retrieving objects as requested in issue #225. This allows standard cloud-native tools, AWS SDKs, and Ceph-compatible clients to interoperate with Momo seamlessly.

## What Changes

- **Add Storage Listing Method:** Extend the `Store` interface and `CASStore` implementation to support scanning the metadata database.
- **Implement S3 ListObjectsV2:** Parse GET requests targeting the bucket root, filter by prefix and delimiter, group by CommonPrefixes, and return S3-compliant XML responses.
- **Implement S3 DeleteObject:** Parse DELETE requests for specific object keys and remove their namespace mapping in BoltDB.
- **Implement S3 GetObject:** Parse GET requests for specific keys, verify their existence, and stream the raw file back to the client.
- **Introduce Handled Request Sentinel:** Add a transport-level sentinel error `ErrRequestHandled` to allow the server daemon to gracefully finish connections that were completely processed at the HTTP/S3 gateway level.
- **Store Injection:** Update the S3 Communicator to receive a reference to the `storage.Store` from the server daemon.

## Capabilities

### New Capabilities
- `s3-list-delete-get`: S3 API capability covering object listing (ListObjectsV2), object deletion, and object retrieval over TCP and QUIC.

### Modified Capabilities
<!-- No requirement changes to existing features -->

## Impact

- **Affected Code:**
  - `src/storage/storage.go`: Extends `Store` and `CASStore`.
  - `src/transport/communicator.go`: Introduces sentinel error.
  - `src/transport/s3_communicator.go`: Adds S3 URL parsing, XML formatter, and HandshakeServer GET/DELETE/OPTIONS handlers.
  - `src/server/server.go`: Intercepts and injects `Store` and catches `ErrRequestHandled`.
- **APIs:** Introduces S3 ListObjectsV2, GetObject, and DeleteObject endpoints over port 4440.
