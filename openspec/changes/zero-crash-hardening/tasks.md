## 1. Analysis & Refactoring
- [x] 1.1 Audit `src/common/replication.go` and `src/server/server.go` for unsafe slice accesses and replace `strconv.Atoi` usage on raw network buffers with safe parsing logic.
- [x] 1.2 Harden `parsePaddedIntFast` in `src/server/file.go` to handle edge cases like entirely null buffers or malformed signs without panicking.
- [x] 1.3 Implement a unified `SafeParseInt` utility in `src/common` to standardize integer extraction from padded byte slices.

## 2. Resource & Panic Protection
- [x] 2.1 Introduce a `defer recover()` middleware pattern for all spawned goroutines in `Daemon` and `ChangeReplicationModeServer`.
- [x] 2.2 Verify that `MaxFileSize` limits are strictly enforced *before* any `io.Copy` or file creation logic executes.

## 3. Configuration Safety
- [x] 3.1 Refactor `loadGlobalConfig` to gracefully handle malformed CSV formats in `replication_order` without crashing or allocating massive slices.

## 4. Verification & Benchmarking
- [x] 4.1 Write a Fuzz Test (`FuzzParsePaddedIntFast`) in `src/server/file_test.go` to aggressively test the padding parser.
- [x] 4.2 Write a Fuzz Test for the metadata extraction logic (`GetMetadata`).
- [x] 4.3 Run local `make benchmark` and compare against `master` to guarantee compliance with the 5% degradation limit.
