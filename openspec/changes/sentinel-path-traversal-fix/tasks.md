# Tasks — Sentinel: Path Traversal Validation Ordering Fix

## Phase 1 — Implementation
- [x] In `src/transport/momo_tcp.go`: Validate `rawHash == "" || common.HasPathTraversalChars(rawHash)` before `SanitizeLog`.
- [x] In `src/transport/momo_tcp.go`: Explicitly close connection on panic recovery (`m.Close()`).
- [x] In `src/transport/momo_quic.go`: Validate `rawHash == "" || common.HasPathTraversalChars(rawHash)` before `SanitizeLog`.
- [x] In `src/transport/momo_quic.go`: Explicitly close connection on panic recovery (`m.Close()`).

## Phase 2 — Testing & Verification
- [x] Add unit test `TestMomoTCPReceiveMetadata_RejectsPathTraversal` in `src/transport/momo_tcp_test.go`.
- [x] Verify full test suite passes with `make test`.
- [x] Clean `go vet` and `gofmt`.
- [x] PR carries `Resolves #997`.
