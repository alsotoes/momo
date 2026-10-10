# Tasks: Pre-merge complexity gates

## Implementation

- [x] `.golangci.yml` (v2): `gocognit` + `gocyclo` at 15, `issues.new: true`
- [x] `.github/workflows/lint.yml`: PR-only golangci-lint job with `only-new-issues`
- [x] `Makefile`: `make lint` target (graceful skip without the binary)
- [x] `docs/CORE/STANDARDS.md`: Complexity budget section

## Testing

- [x] `golangci-lint config verify` passes
- [x] `make lint` reports 0 issues on master (new-code mode)
- [x] Full-mode proof run: 19 legacy findings in storage, correctly ignored in new-code mode
- [x] Lint CI job green on this PR (36s)

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] `no-blog` justification (Rule 76) — CI tooling, no product narrative
- [x] PR links tracking issue #1172 (Rule 11)
