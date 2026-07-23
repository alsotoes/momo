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

## Production & Multi-Tenancy Considerations
1. **Path Normalization & Sanitization:** Virtual paths MUST be normalized prior to indexing (e.g., stripping leading/trailing slashes, resolving `//` to `/`, and trimming whitespace) to prevent malicious path formatting or duplicate variations in indexing.
2. **Namespace Overwrite Policy:** If a client uploads a file to an existing virtual path (`RemotePath`), the storage engine MUST support a strict conflict policy: either fail explicitly to prevent accidental loss, or overwrite (updating the path-to-hash pointer and decrementing the reference count of the old CAS block).
3. **Isolated Deduplication Boundaries:** For enterprise multi-tenancy, global CAS deduplication can leak metadata across tenants via timing attacks (side-channel). Future iterations MUST support tenant-level scoping of content-hash indexes, ensuring deduplication is isolated strictly within a tenant's namespace boundary.

## Impact
- **Affected Specs:** `specs/storage/spec.md` (added metadata requirements).
- **Affected Code:** `src/common/struct.go`, `src/client/client.go`, `src/server/server.go`, `src/storage/storage.go`.

Resolves https://github.com/alsotoes/momo/issues/227