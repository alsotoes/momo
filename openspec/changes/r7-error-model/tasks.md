# Tasks: R7 — Error Model + Ops (ENOSPC, Exit Codes, Cluster Health)

## Implementation

### ENOSPC Signaling
- [ ] `src/storage/local_blobstore.go`: Add `checkDiskSpace()` before `PutBlob`; return `syscall.ENOSPC` on `ENOSPC`
- [ ] `src/storage/storage.go`: Propagate `ENOSPC` from `BlobStore` up through `CASStore.Put`
- [ ] `src/transport/s3_communicator.go`: Map `ENOSPC` → `507 Insufficient Storage` in `writeS3Error`
- [ ] `src/transport/momo_tcp.go` / `momo_quic.go`: Propagate `ENOSPC` in native protocol handlers

### Distinct Exit Codes
- [ ] `src/momo.go`: Define `ExitConfigError = 2`, `ExitNetworkError = 3`, `ExitStorageError = 4`, `ExitCryptoError = 5`, `ExitP2PError = 6`
- [ ] `src/momo.go` `runServer`: Return appropriate code on config parse, bind, storage init failure
- [ ] `src/momo.go` `runClient`: Return appropriate code on config, connection failure
- [ ] `src/momo.go` `runFuseMount`: Return appropriate code on mount, FUSE init failure

### Cluster Health Endpoint
- [ ] `src/server/metrics_exporter.go`: Add `handleDetailedHealth` returning JSON with all required fields
- [ ] `src/server/server.go`: Register `/health/detailed` route
- [ ] `src/p2p/peer_map.go`: Export `GetPeerCounts() (alive, suspect, offline int)`
- [ ] `src/storage/storage.go`: Export `GetDiskUsage() (used, free uint64)` via `bbolt.Stats()`
- [ ] `src/server/replication.go`: Export `GetReplicationStatus() (mode string, pending int)`
- [ ] `src/storage/integrity.go`: Export `GetScrubStatus() (running bool, lastRun int64, errors int)`

## Testing

- [ ] Unit test: `TestENOSPCSignaling` simulates disk full via mock `BlobStore`
- [ ] Unit test: `TestExitCodes` verifies each failure path returns correct code
- [ ] Integration test: `TestHealthDetailedEndpoint` verifies all JSON fields present
- [ ] `go test -race ./...` — all tests pass
- [ ] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #935` (Rule 11)