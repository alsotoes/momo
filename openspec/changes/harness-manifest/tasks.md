# Tasks: Harness manifest + validation gate

## Implementation

- [x] `.golangci.yml` already exists with gocognit/gocyclo
- [x] `openspec/ai-workflow.yaml` (manifest) — at repo root, version 1
- [x] Project `opencode.json` — add `plugins: ["@dietrichgebert/ponytail@5.1.0"]`
- [x] `.github/scripts/ai_workflow_check.py` — 7 validation checks
- [x] `test_ai_workflow_check.py` — unit tests for the checker
- [x] `Makefile` — `ai-workflow-check` target + PHONY
- [x] `.github/workflows/ai_workflow_check.yml` — PR-only workflow with `only-new-issues` equivalent
- [x] `AGENTS.md` + `docs/README.md` + `AI_FLYING_SOLO.md` — reference lines
- [ ] `docs/CORE/STANDARDS.md` — add harness manifest section
- [ ] `docs/CORE/STANDARDS.md` — complexity budget section (already done)

## Testing

- [x] `golangci-lint config verify` passes
- [x] `make lint` → 0 issues on master (new-code mode)
- [x] `make ai-workflow-check` → 0 issues on current code
- [x] `go test ./...` — full suite passes
- [x] `make blog-check`, `make adr-sync-check`, `make diagram-check` — all pass

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rules 77/78)
- [ ] Blog post OR no-blog justification (Rule 76) — CI tooling → no-blog (ADR 0064 precedent)
- [ ] PR body includes `Resolves #1174` (Rule 11)
