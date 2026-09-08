# Tasks: Go 1.26 Upgrade

## Implementation

- [x] Create GitHub issue #1064 and OpenSpec change
- [ ] Update root `go.mod` to `go 1.26.x`
- [ ] Update root `go.work` to `go 1.26.x`
- [ ] Update `.github/workflows/go.yml` → `go-version: '1.26.x'`
- [ ] Update `.github/workflows/smoke_test.yml` (4 occurrences)
- [ ] Update `.github/workflows/encryption_smoke_test.yml` (4 occurrences)
- [ ] Update `.github/workflows/external_client_test.yml`
- [ ] Update `.github/workflows/metrics_test.yml`
- [ ] Update `.github/workflows/p2p_test.yml`
- [ ] Update `.github/workflows/scale_cas_test.yml`
- [ ] Update `.github/workflows/storage_backends_test.yml`
- [ ] Update `.github/workflows/weekly_sanity.yml`
- [ ] Update `.github/workflows/verify_go_version.yml` logic
- [ ] Run `go work sync` to sync all module go.mod files
- [ ] Run `go work vendor` for vendor parity (Rule 25)

## Testing

- [ ] `go build ./...` — all modules build
- [ ] `go vet ./...` — clean
- [ ] `gofmt -l` — clean
- [ ] `make test` — full suite passes with `-race`
- [ ] `make blog-check` — 47 posts valid
- [ ] `make diagram-check` — 27 diagrams valid
- [ ] `make adr-sync-check` — parity OK

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #1064` (Rule 11)
- [ ] PR merges with `--merge --delete-branch` (Rule 55)