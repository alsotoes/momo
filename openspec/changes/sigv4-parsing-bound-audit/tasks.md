# Tasks — SigV4 Parsing Bound Audit

## Phase 1 — Implementation
- [x] Change `parseSigV4AuthHeader` to return `(sigV4Components, error)` with `syscall.EBADMSG`.
- [x] Update `verifySigV4Signature` call site.
- [x] Update `S3Communicator.HandshakeServer` call sites (auth parse + streaming context).
- [x] Document the bound chain in `parseSigV4AuthHeader` (no iteration caps added: unreachable).
- [x] Record the audit finding in `.jules/sentinel.md`.

## Phase 2 — Testing & Verification
- [x] Add `TestParseSigV4AuthHeader` (valid + malformed table).
- [x] Add `TestHandshakeServer_AuthHeaderBound` (64 KiB handshake cap rejects oversized header; valid header accepted).
- [x] `go build ./...`, `go vet`, `gofmt` clean.
- [x] `go test -race ./src/transport/` passes.
- [x] PR carries `Resolves #1231`.
