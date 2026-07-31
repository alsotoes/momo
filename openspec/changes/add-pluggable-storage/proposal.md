# Change: Pluggable Storage Backend (local/NFS/S3/raw device)

**Related Issues:** https://github.com/alsotoes/momo/issues/226

## Why
The storage backend was hardcoded to the local filesystem. To support NFS, S3-compatible APIs, and raw block devices, the blob storage layer must be pluggable. Default behavior (local path) must be preserved when unconfigured.

## Technical Architecture

### 1. Two-Layer Split: BlobStore + MetadataStore
The `CASStore` is refactored into two layers:
- **BlobStore** (pluggable): Raw blob bytes keyed by content hash. Implementations: `LocalBlobStore`, `S3BlobStore`, `RawBlobStore`.
- **MetadataStore** (fixed): Bbolt per-node metadata (name→hash, refcounts, tombstones, GC, P2P exchange). Unchanged for all backends.

### 2. StorageFactory
A `NewStore(cfg, daemon)` factory mirrors `ProtocolFactory`, switching on `cfg.Storage.Backend` to select the blob backend. GC is started internally.

### 3. Streaming Replication Forward
`GetBlobPath` was removed from the `Store` interface. Chain/Splay forwarding now uses `store.Get()` → `connectToPeerStream(io.Reader)`, making replication backend-agnostic.

### 4. Backends
| Backend | Implementation | New deps | Config |
|---------|---------------|----------|--------|
| `local` (default) | `LocalBlobStore` (tiered FS) | none | `daemon.data` |
| `nfs` | Same as `local` on NFS mount | none | `daemon.data` on NFS |
| `s3` | `S3BlobStore` (zero-dep SigV4 HTTP client) | none | `s3_*` fields |
| `raw` | `RawBlobStore` (direct block I/O + bbolt alloc table) | none | `raw_device_path` or `daemon.drive` |

## What Changes
- **`src/storage/blobstore.go`**: New `BlobStore` interface (PutBlob/GetBlob/DeleteBlob).
- **`src/storage/local_blobstore.go`**: Extracted FS logic from `CASStore`.
- **`src/storage/s3_blobstore.go`**: S3 backend with minimal SigV4 client.
- **`src/storage/raw_blobstore.go`**: Raw device backend with bump allocator.
- **`src/storage/factory.go`**: `NewStore` factory.
- **`src/storage/storage.go`**: `CASStore` refactored to compose `BlobStore` + bbolt.
- **`src/storage/gc.go`**: Uses `BlobStore.DeleteBlob` instead of `os.Remove`.
- **`src/common/struct.go`**: `ConfigurationStorage` expanded with backend fields.
- **`src/common/config.go`**: `loadStorageConfig` parses new fields.
- **`src/server/server.go`**: Uses `storage.NewStore` instead of `NewCASStore`.
- **`src/client/client.go`**: Added `ConnectStream` for streaming forwarding.
- **`Store` interface**: `GetBlobPath` removed (replaced by streaming forward).

## Impact
- **Performance**: Zero new dependencies. S3 client is ~200 lines of stdlib-only SigV4.
- **Reliability**: Bbolt metadata stays local per-node; GC/refcount/tombstone logic unchanged.
- **Compatibility**: Default `backend=local` preserves existing behavior exactly.
- **Rule 33**: All backends work with all 4 transports (same `Store` interface).

## Verification
- Unit tests for each `BlobStore` implementation (local, S3, raw).
- Integration test via `NewStore` factory for each backend.
- S3 tested with in-process `httptest` mock server.
- Raw tested with temp file as fake block device + persistence across restart.
- All existing tests pass unchanged with `LocalBlobStore`.
