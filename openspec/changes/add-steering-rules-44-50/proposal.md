# Proposal: Add Steering Rules 44-50 (AI Agent Hygiene & Pipeline Integrity)

**Related Issue:** https://github.com/alsotoes/momo/issues/324

- **Champion:** alsotoes
- **Status:** `Draft`

## 1. Problem

Across PRs #281, #284, #288, #290, #292, and #294, a recurring set of destructive behaviors by automated maintenance agents (Jules) and pipeline gaps were observed that are not covered by the existing 43 steering rules. These caused significant manual remediation effort on every PR:

1. **Learning file wipes**: Jules replaced the entire contents of `.jules/bolt.md` and `.jules/sentinel.md` with a single new entry, deleting all historical knowledge (101+ lines lost on PR #294).
2. **Planning artifacts committed**: `plan.md` was included in PRs #284, #288, #290, #294 despite being a transient working file.
3. **Pipeline file reverts**: Jules destructively reverted `gemini_reviewer.yml` and `ai_reviewer.py` fixes on PRs #284, #290, breaking the bot review identity and issue assignment.
4. **Master file deletions**: Jules deleted skill files, test files, and source files that exist on master (PRs #284, #290).
5. **Duplicate code re-introduction**: Jules re-introduced code already on master (e.g., `AppendPaddedInt`, `binary.Write` optimization on PR #290), causing unnecessary conflicts.
6. **Orphaned tracking issues**: Autonomously created issues were missing assignee and `automation` label (PRs #288, #290, #292, #294).
7. **Bot review identity regression**: Reviews were posted as `alsotoes` instead of `github-actions[bot]` due to PAT token being used for review posting instead of `GITHUB_TOKEN` (PRs #284, #288, #290, #292, #294).
8. **Auto-generated file conflicts**: `benchmark_history.csv` and `PERFORMANCE.md` caused merge conflicts on every PR due to pre-commit hook regeneration.

## 2. Proposed Solution

Add seven new steering rules (44-50) to `openspec/config.yaml` that codify the corrective practices manually applied across all reviewed PRs. These rules establish:

- **Knowledge file integrity** (Rule 44): Append-only semantics for `.jules/` learning files.
- **Artifact hygiene** (Rule 45): Transient planning files excluded from PRs.
- **Pipeline scope limitation** (Rule 46): AI agents forbidden from modifying infrastructure files.
- **Master preservation** (Rule 47): AI agents forbidden from deleting master files.
- **Review identity** (Rule 48): Bot token required for review posting to maintain `github-actions[bot]` identity.
- **Issue assignment** (Rule 49): Autonomously created issues must be assigned and labeled at creation.
- **Pre-push sync** (Rule 50): Mandatory master merge with auto-generated file conflict resolution strategy.

## 3. Technical Spec

### Changes to `openspec/config.yaml`

Append rules 44-50 to the `Project Steering Rules` list under the `context` block. Each rule follows the existing format: **Rule Name**: Description with MUST/PROHIBITED keywords.

### No Code Changes

This proposal does not modify any application code, CI/CD pipelines, or test suites. It only updates the steering rules configuration.

## 4. Impact

- **Affected Code:** `openspec/config.yaml` only.
- **APIs:** None.
- **Tests:** None.
- **CI/CD:** None directly. The rules will be enforced by the Gemini AI Reviewer on subsequent PRs.

## 5. Verification

- Confirm `openspec/config.yaml` is valid YAML after editing.
- Confirm rules are numbered 44-50 sequentially.
- Confirm no duplication with existing rules 1-43.

---
