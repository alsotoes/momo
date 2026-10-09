# Tasks: Unified panic-recovery helpers (Rules 37 & 43)

## Implementation

- [x] Add `src/common/recover.go` (`RecoverErr`, `RecoverErrWith`, `RecoverErrClose`, `RecoverClose`)
- [x] Adopt the helpers across transport (S3, QUIC)
- [x] Adopt the helpers across storage (CASStore, local/raw/encrypted/s3 blobstores)
- [x] Adopt the helpers across server (Daemon, getFile, replication, server)
- [x] Adopt the helpers across client and common (net, rotation)
- [x] Replace the `#1151` local `recoverChangeReplicationPanic` helper with `common.RecoverClose`
- [x] Document the standard in `docs/CORE/STANDARDS.md` (§9)

## Testing

- [x] `src/common/recover_test.go`: EIO mapping, custom errno, close-on-panic, no-panic paths
- [x] Update the storage panic-recovery test to assert on the POSIX mapping
- [x] `make test` (vet + race + cover) passes across all 9 modules

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] PR links tracking issue #1160 (Rule 11)
