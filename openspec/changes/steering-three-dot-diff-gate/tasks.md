# Tasks: steering-three-dot-diff-gate — three-dot diff gate, anti-rogue merge check, CI-freeze notes (#944)

## 1. Steering rules (`openspec/config.yaml`)
- [ ] **Rule 55**: append pre-merge gate — verify `git diff master...HEAD --name-only`
      contains ONLY the PR's intended files; unexpected master-file
      deletion/alteration MUST be investigated and reverted before merge (SDG-T1)
- [ ] **Rule 62**: replace two-dot `gh pr diff --name-only` with three-dot
      `git diff master...HEAD --name-only` in the staleness check (SDG-T2)
- [ ] **Rule 73**: document that perf optimizations with a benchmark surface do
      NOT qualify for the trivial-refactor exemption; treated as enhancements
      (full OpenSpec + linked issue required) (SDG-T3)

## 2. Workflow doc (`docs/AI_FLYING_SOLO.md`)
- [ ] Add WP1 "merge-inflation" pitfall: use three-dot to enumerate PR's own
      changes after Rule 50 merge (SDG-T4)
- [ ] Add WP2 "frozen gh pr checks" pitfall: cross-check `gh api .../jobs` when a
      check stays pending with frozen `updatedAt` (SDG-T5)

## 3. Reviewer script audit (Rule 70)
- [ ] Confirm `.github/scripts/ai_reviewer.py` uses three-dot diff (lines 13, 161);
      no diff-scope bug. No script change required (SDG-T6)

## 4. Docs / spec (Rule 27 / Rule 73)
- [x] Author `openspec/changes/steering-three-dot-diff-gate/{proposal,spec,tasks}`
      linked to issue #944 (SDG-T7)

## 5. Validation
- [ ] `git diff master...HEAD --name-only` on this PR shows only config.yaml, the
      AI_FLYING_SOLO.md pitfalls, and the OpenSpec change set (SDG-T1 re-check)
- [ ] CI green including `review`
