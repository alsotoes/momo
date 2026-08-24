# Tasks: Core Integrity Verification (#903)

## 1. Phase 1: data model
- [ ] Define `ChecksumRef{Algo, Value}`; add `FileMetadata.Checksums []ChecksumRef`
- [ ] Marshaling for additivite carry (feed `X-Momo-S3-Meta` / metadata path)

## 2. Phase 2: centralized verifier
- [ ] Generic verifier at store/ingest layer; invoke from shared `getFile`
- [ ] Confirm inert (no double-hash) when no additive checksum supplied
- [ ] Re-verify on replication-hop ingest

## 3. Phase 3: retrieval bit-rot check (opt-in)
- [ ] `store.Get` optional verification path (opt-in, default off)

## 4. Phase 4: surface adaptation
- [ ] Map S3 `x-amz-checksum-*` → `ChecksumRef`; migrate `parseChecksum`/
      `FinalizeIntegrityChecksum` specifics into the S3 adapter
- [ ] Generalize `transport.ChecksumFinalizer` seam beyond S3; keep native surface no-op

## 5. Phase 5: docs + tests + gates
- [ ] Update `docs/ARCHITECTURE.md`, `docs/COMPATIBILITY.md` (parity, Rule 27)
- [ ] Tests: ingest mismatch (any surface), no-checksum inert, replica reject,
      bit-rot `Get`, S3 adapter mapping, native no-op
- [ ] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff
- [ ] `benchstat` gate — no regression on measured hot paths
