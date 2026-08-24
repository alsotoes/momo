# Tasks: Steering Rule 73 — Spec-First Implementation Mandate (#908)

## 1. Phase 1: steering rule
- [x] Add Rule 73 to `openspec/config.yaml` (single source of truth, Rule 39)

## 2. Phase 2: workflow doc
- [x] Add Rule 73 to `docs/AI_FLYING_SOLO.md` reference list
- [x] Add Step 1b "Author OpenSpec Change (features)"; cycle → 17 steps

## 3. Phase 3: reviewer sync (Rule 70)
- [x] `pr_has_label` + `has_openspec_change` helpers in `ai_reviewer.py`
- [x] Flag enhancement PRs without an OpenSpec change; withhold approval/merge
- [ ] Confirm `gemini_reviewer.yml` needs no change (injected into script prompt only)

## 4. Phase 4: governance artifact + merge
- [x] Author this OpenSpec change (`steering-rule-73-spec-first`) mirroring #908 (Rule 11)
- [ ] Sync master, open PR (Resolves #908), reviewer + CI, merge
- [ ] Post-merge: update #908 status comment (Rule 22)
