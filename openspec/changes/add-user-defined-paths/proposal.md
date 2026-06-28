# Change: Support User-Defined Paths via Metadata

## Why
Users need a way to organize and reference uploaded files using human-readable, hierarchical directory paths (e.g., `customer01/documents/invoice.pdf`). 
Storing these paths directly on the storage node filesystems (hierarchical path storage) breaks Content-Addressable Storage (CAS) deduplication, degrades load-balancing efficiency via the CRUSH-lite algorithm, and violates Rule 12 (Object Storage Paradigm).
By storing user-defined paths purely as virtual metadata mapping to the content hash, we can deliver complete logical organization without breaking Momo's core algorithmic scalability or deduplication capabilities.

## What Changes
- **Client Metadata Payload:** Extend the client-side metadata upload packet to accept and transmit an optional `RemotePath` string.
- **Client CLI flag:** Add a `--remote-path` flag to the client upload CLI command to allow users to specify custom virtual folder paths.
- **Server Bbolt Indexing:** Update the server's Bbolt metadata store (`CASStore`) to securely index and retrieve the `RemotePath` mapping of each content-addressed object.
- **Standard Linkage:** Link this specification to Issue #227.

## Impact
- **Affected Specs:** `specs/storage/spec.md` (added metadata requirements).
- **Affected Code:** `src/common/struct.go`, `src/client/client.go`, `src/server/server.go`, `src/storage/storage.go`.

Resolves https://github.com/alsotoes/momo/issues/227