## 1. Implementation
- [x] 1.1 Create `BlobStore` interface (PutBlob/GetBlob/DeleteBlob).
- [x] 1.2 Extract `LocalBlobStore` from `CASStore` (tiered FS logic).
- [x] 1.3 Refactor `CASStore` to compose `BlobStore` + bbolt metadata.
- [x] 1.4 Add `StorageFactory` (`NewStore` switching on `cfg.Storage.Backend`).
- [x] 1.5 Add `Backend` + S3/raw config fields to `ConfigurationStorage`.
- [x] 1.6 Replace `NewCASStore` call in `server.go` with `NewStore`.
- [x] 1.7 Refactor `gc.go` to use `BlobStore.DeleteBlob`.
- [x] 1.8 Add `ConnectStream` to `client.go` for streaming forwarding.
- [x] 1.9 Refactor Chain/Splay forwarding to use `store.Get()` + `connectToPeerStream`.
- [x] 1.10 Remove `GetBlobPath` from `Store` interface.
- [x] 1.11 Implement `S3BlobStore` with zero-dep SigV4 client.
- [x] 1.12 Implement `RawBlobStore` with bump allocator + bbolt alloc table.
- [x] 1.13 Wire up S3 and raw backends in factory.

## 2. Testing
- [x] 2.1 Unit tests for `LocalBlobStore` (via existing `CASStore` tests).
- [x] 2.2 Unit tests for `S3BlobStore` (in-process mock S3 server).
- [x] 2.3 Unit tests for `RawBlobStore` (temp file fake device + persistence).
- [x] 2.4 Factory selection tests (`TestNewStore_*`).
- [x] 2.5 Integration test for S3 and raw via `NewStore` factory.
- [x] 2.6 All existing tests pass unchanged.

## 3. Documentation
- [x] 3.1 Update `docs/CONFIGURATION.md` with backend field + examples.
- [x] 3.2 Update `docs/ARCHITECTURE.md` with BlobStore/MetadataStore split.
- [x] 3.3 Update `docs/STANDARDS.md` with backend interface contract.
- [x] 3.4 Update `docs/README.md` features list.
- [x] 3.5 Update `conf/momo.conf` with backend examples.
- [x] 3.6 Create `openspec/changes/add-pluggable-storage/` spec.
- [x] 3.7 Post design summary on issue #226.
