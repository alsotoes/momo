# Change: Steering Rule 73 — Spec-First Implementation Mandate
**Related Issues:**
- https://github.com/alsotoes/momo/issues/908

## Why
Feature/spec-driven work occasionally shipped with only a tracking issue and no
OpenSpec change, or authored the spec after the code. To make spec-first
governance a hard gate, all new implementations MUST document their design in an
OpenSpec change linked to a GitHub issue before code lands.

## What Changes
- Add **Rule 73 — Spec-First Implementation Mandate** to `openspec/config.yaml`:
  - ALL new features / spec-driven changes / architectural shifts MUST first
    author `openspec/changes/<id>/{proposal.md, specs/<id>/spec.md, tasks.md}`.
  - The spec `spec.md` MUST link the GitHub issue at the top (Rule 11).
  - The implementing PR MUST include the OpenSpec change and use
    `Resolves #ISSUE_ID`; or reference an already-ratified spec.
  - Only trivial internal bug fixes / no-behavioral-surface refactors are exempt
    from a formal spec, but MUST still have a tracking issue.
- Update `docs/AI_FLYING_SOLO.md`:
  - Add Rule 73 to the Steering Rules Reference list.
  - Add **Step 1b: Author OpenSpec Change (features)** (cycle becomes 17 steps).
- Sync the AI reviewer (Rule 70): `.github/scripts/ai_reviewer.py` now flags
  enhancement PRs that merge without an OpenSpec change under `openspec/changes/`.

## Non-Goals
- No runtime/protocol/storage behavior change — governance/process only.
- No changes to `gemini_reviewer.yml` (Rule 73 is enforced inside `ai_reviewer.py`;
  the workflow already passes PR context and needs no new event/label handling).
- Not retroactive: existing in-flight PRs (#902/#906 already merged, #904 spec
  submission) are unaffected.
