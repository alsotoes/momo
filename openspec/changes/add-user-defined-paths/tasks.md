## 1. Implementation

- [ ] Extend `common.FileMetadata` in `src/common/struct.go` to include a `RemotePath` field.
- [ ] Add the `--remote-path` string flag to the client upload command.
- [ ] Incorporate `RemotePath` into the metadata transmission and parsing logic of the S3 and standard TCP/QUIC communicators.
- [ ] Update Bbolt indexing logic in `src/storage/storage.go` to save and load `RemotePath` alongside name and hash.
- [ ] Create unit tests asserting that uploading a file with a user-defined remote path successfully indexes it and matches on retrieve.

## 2. Verification

- [ ] Execute `go test -v ./src/storage/...` to verify Bbolt index updates.
- [ ] Execute `make test` to ensure no regressions in existing transport or client pipelines.
- [ ] Run `openspec validate add-user-defined-paths --strict` to verify spec conformity.