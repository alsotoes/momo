# Change: ECC Tools ↔ AI Reviewer Integration

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1134 (tracking)

## Why

The ECC Tools GitHub App audits every PR and posts one comment per dimension — Security
Evidence, PR Risk Taxonomy, Reference Set Readiness, Hosted Promotion Readiness — each
carrying a marker of the form
`<!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->`. Those results are currently
disconnected from momo's own reviewer, so a human has to read two places to see the full
picture. Separately, the app can open unsolicited "ECC bundle" PRs (see #1126) that always
target harnesses momo does not use (Claude Code / Codex); triaging them by hand is noise.

This change wires the app into momo's automation in two narrow, advisory directions.

## What Changes

- **`.github/scripts/ai_reviewer.py`** — new `get_ecc_audit_summary(pr_number)`:
  - reads the PR's `ecc-tools` audit comments,
  - keeps the latest verdict per dimension,
  - appends a compact `ECC audit — …` block to the posted review.
  - **Advisory only:** the approve/auto-merge decision (`is_approved`) is computed before
    the block is appended and is never affected by ECC's verdicts.
- **`.github/workflows/ecc_bundle_triage.yml`** (new) — on a `pull_request` opened/reopened/
  synchronize from `ecc-tools[bot]` whose title matches "ECC bundle", post a rationale
  comment and close the PR. Defense-in-depth for `autoPR: false` in `.github/ecc-tools.yml`.
- **`.github/scripts/test_ai_reviewer_ecc.py`** (new) — unit tests for the audit parser.

## Out of Scope

- Having momo's bot *trigger* ECC (`/ecc-tools …` comments). ECC already auto-audits every
  PR on the `pull_request` event, so an explicit trigger would only duplicate comment noise.

## Impact

- Reviewer output gains one summary line when ECC has audited the PR; absent gracefully
  otherwise.
- Unsolicited ECC bundle PRs are closed automatically with a clear rationale.
- No change to merge gating: ECC remains advisory; Rule 70 (reviewer) and Rule 55 (merge
  gate) stay authoritative.
