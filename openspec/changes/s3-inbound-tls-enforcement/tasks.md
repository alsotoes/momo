# Tasks: S3 — require TLS (or explicit insecure) for the s3-tcp inbound gateway (issue #775)

## 1. Phase 1: `Listen()` TLS enforcement for s3-tcp
- [x] 1.1 Split `momo-tcp`/`s3-tcp` cases in `Listen()`.
- [x] 1.2 For `s3-tcp`: if `f.tlsConfig == nil` and `!TLSInsecure` → return `EINVAL` error.
- [x] 1.3 For `s3-tcp`: if `f.tlsConfig == nil` and `TLSInsecure` → log warning, accept cleartext.
- [x] 1.4 For `momo-tcp`: unchanged (never fails on missing TLS config).
- [x] 1.5 For QUIC self-signed fallback: log warning about unauthenticated identity.

## 2. Phase 2: Tests
- [x] 2.1 `TestProtocolFactory_Listen_S3TCP_TLSEnforcement`: s3-tcp without TLS rejected by default.
- [x] 2.2 s3-tcp with `TLSInsecure=true` accepted.
- [x] 2.3 momo-tcp without TLS still accepted (unchanged).
- [x] 2.4 Fix `TestS3Communicator_FullFlow` (uses s3-tcp without TLS) to set `TLSInsecure: true`.

## 3. Phase 3: Docs
- [x] 3.1 Update `docs/PROTOCOL.md` — document s3-tcp TLS requirement and QUIC self-signed warning.
- [x] 3.2 Create `openspec/changes/s3-inbound-tls-enforcement/` (proposal, tasks) with `Resolves #775` links.

## 4. Validation
- [ ] 4.1 Run `gofmt`, `go vet`, and the full per-module test suites.
- [ ] 4.2 Verify `go work vendor` produces no diff (Rule 25).
- [ ] 4.3 Commit (pre-commit hook syncs `docs/PERFORMANCE.md`), Rule 58 branch check, push.
- [ ] 4.4 Open PR with `Resolves #775`, wait for checks, address review, merge, close issue, Rule 71 gate.

