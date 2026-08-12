# Tasks: S3 — enforce HTTPS (or gated insecure) for the S3 storage backend endpoint (issue #774)

## 1. Phase 1: Config + endpoint scheme validation
- [x] 1.1 Add `S3Insecure bool` field to `ConfigurationStorage` in `src/common/struct.go`.
- [x] 1.2 Parse `s3_insecure` from INI `[storage]` section in `src/common/config.go` `loadStorageConfig`.
- [x] 1.3 In `NewS3BlobStore`: validate `s3_endpoint` scheme. Require `https://`; allow `http://` only when `S3Insecure` is true (with `log.Printf` warning); reject missing/unsupported scheme with `EINVAL`.

## 2. Phase 2: Tests
- [x] 2.1 Update all existing tests that use `httptest.NewServer` (plain HTTP) to set `S3Insecure: true`.
- [x] 2.2 Add `TestS3BlobStore_TLSEnforcement` with subtables: https accepted, http rejected without insecure, http accepted with insecure, missing scheme rejected, unsupported scheme rejected.

## 3. Phase 3: Docs
- [x] 3.1 Update `docs/CONFIGURATION.md` — document `s3_insecure` knob.
- [x] 3.2 Update `docs/PROTOCOL.md` — add outbound TLS enforcement section and layered confidentiality model.
- [x] 3.3 Create `openspec/changes/s3-https-enforcement/` (proposal, tasks) with `Resolves #774` links.

## 4. Validation
- [ ] 4.1 Run `gofmt`, `go vet`, and the full per-module test suites (`src/root`, `common`, `transport` incl. `-race`, `client`, `server`, `storage`, `metrics`, `crypto`, `p2p`).
- [ ] 4.2 Verify `go work vendor` produces no diff (Rule 25).
- [ ] 4.3 Commit (pre-commit hook syncs `docs/PERFORMANCE.md` / `.github/data/benchmark_history.csv`), Rule 58 branch check, push.
- [ ] 4.4 Open PR with `Resolves #774`, wait for checks, address the `github-actions` review, merge `--merge --delete-branch`, close the issue, run the Rule 71 master gate.

