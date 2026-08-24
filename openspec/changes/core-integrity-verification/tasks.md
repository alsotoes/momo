# Tasks: Core Integrity Verification (#903)

## 1. Phase 1: data model
- [x] Define `ChecksumRef{Algo, Value}`; add `FileMetadata.Checksums []ChecksumRef`
- [x] Streaming verifier (`ChecksumSet`, `VerifyStream`) in `common/checksum.go`

## 2. Phase 2: centralized verifier
- [x] Generic verifier invoked from shared `getFile` (protocol-agnostic, `ChecksumProvider`)
- [x] Inert when no additive checksum supplied (no double-hash on common path)
- [x] Re-verify on replication-hop ingest (forwarded `S3Headers` → `ChecksumExpectations` → `getFile`)

## 3. Phase 3: retrieval bit-rot check (opt-in)
- [x] `CASStore.VerifyChecksum` opt-in verification (streams, no-op on empty)

## 4. Phase 4: surface adaptation
- [x] Map S3 `x-amz-checksum-*` → `ChecksumRef` (`ChecksumExpectations` + marker always persisted)
- [x] Replace S3 `ChecksumFinalizer`/`Read`-hashing with `transport.ChecksumProvider`; native surface no-op

## 5. Phase 5: docs + tests + gates
- [x] Update `docs/ARCHITECTURE.md` (parity, Rule 27)
- [x] Tests: common `ChecksumSet`/`VerifyStream`, `CASStore.VerifyChecksum`,
      `ChecksumExpectations`, mismatch hook, single-part BadDigest (server), `go test ./...`
- [x] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff pending
- [ ] `benchstat` gate — no regression on measured hot paths
