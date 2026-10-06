# Tasks: ECC Tools ↔ AI Reviewer Integration

## 1. Reviewer consumes ECC audits (Direction 2)
- [x] Add `get_ecc_audit_summary(pr_number)` to `.github/scripts/ai_reviewer.py`
- [x] Parse `<!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->` markers
- [x] Keep the latest verdict per dimension; map status → ✅/⚠️/❌
- [x] Append the block to the posted review; keep it advisory (no effect on `is_approved`)

## 2. Auto-triage ECC bundle PRs (Direction 3)
- [x] Add `.github/workflows/ecc_bundle_triage.yml`
- [x] Trigger on `pull_request` opened/reopened/synchronize
- [x] Guard: author `ecc-tools[bot]` AND title matches "ECC bundle"
- [x] Post a rationale comment and close the PR

## 3. Tests
- [x] Add `.github/scripts/test_ai_reviewer_ecc.py` covering the audit parser
  (multi-dimension, latest-per-dimension, status mapping, no-ECC-comments)

## 4. Governance
- [x] OpenSpec change linked to issue #1134
- [x] ADR generated via `make adr-sync`
- [x] `no-blog` justification (CI tooling; journal is product-engineering only)

## 5. Verification
- [x] `python3 .github/scripts/test_ai_reviewer_ecc.py` passes
- [x] `python3 -c "import ast; ast.parse(open('.github/scripts/ai_reviewer.py').read())"` passes
- [x] Workflow YAML parses
- [x] `make blog-check`, `make diagram-check`, `make adr-sync-check` pass
