# Tasks — Steering Rule 90: Auto-Trace Issue Deduplication

## Phase 1 — Implementation
- [x] Add `find_existing_auto_trace(pr_number, pr_title)` to `ai_reviewer.py`:
      searches OPEN auto-trace issues by `[Auto-Trace] <PR title>` or body reference
      `for PR #<n>`, returns canonical issue number.
- [x] Add `get_current_pr_body(pr_number)` to `ai_reviewer.py`: fetches live PR body
      from GitHub API.
- [x] Make `create_missing_issue` deduplicate: reuse existing canonical issue and link
      `Resolves #<canonical>`; only create when none exists.
- [x] Use `get_current_pr_body` for the `has_issue_link` check in `main()`.
- [x] Add `concurrency` block (`cancel-in-progress: true`, per PR) to
      `gemini_reviewer.yml`.

## Phase 2 — Governance
- [x] Add Rule 90 to `openspec/config.yaml` after Rule 89.
- [x] Update `docs/AI_FLYING_SOLO.md`:
      - Rule 90 reference list entry.
      - Step 1c auto-trace check guidance.
      - Rule 80 Auto-Trace Consolidation Protocol prevention note.
      - Common Pitfalls automated-duplicate variant entry.

## Phase 3 — Verification
- [x] `python3 -m py_compile .github/scripts/ai_reviewer.py` clean.
- [x] `find_existing_auto_trace` verified against PR #996 → returns existing issue
      number (dedup confirmed).
- [x] `openspec/config.yaml` and `gemini_reviewer.yml` YAML-parse clean.
- [x] Incident cleanup: 52 duplicate auto-trace issues (#998–#1054) closed as dups of
      canonical #997; PR #996 linked `Resolves #997`.
- [x] PR #1058 merged (commit `1f1516b1`); issue #1057 closed.