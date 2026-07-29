# AI Flying Solo — Autonomous Development Workflow

This document defines the complete workflow for AI agents working autonomously on bug fixes **and** new features without direct human supervision. All AI agents MUST follow this guide when operating in "Flying Solo" mode.

## Steering Rules Reference

This document is governed by the steering rules in [`openspec/config.yaml`](../openspec/config.yaml) (single source of truth, Rule 39). The following rules are specifically relevant:

- **Rules 14, 18**: 3-push circuit breaker — automated agent loop halts after 3 failed iterations; manual intervention required
- **Rule 20**: Autonomous traceability — reuse existing tracking issues, never create duplicates
- **Rule 25**: Workspace vendoring parity — `go work vendor` must produce no diff
- **Rules 49, 51, 52**: Issue/PR assignment and labeling (assignee: alsotoes, labels: bug|enhancement + automation)
- **Rules 51-57**: PR workflow (assign, label, comment, update, merge, close, return) — applies to both bug fixes and features
- **Rule 58**: Pre-push branch validation
- **Rule 59**: No-assumption doubt protocol (AIFS)
- **Rule 60**: This document is the authoritative workflow guide
- **Rule 61**: Clean rebase via cherry-pick (avoid pre-commit hook spurious commits on force-push)
- **Rule 62**: Stale reviewer re-trigger (comment to re-run Gemini review after push)
- **Rule 63**: Post-merge branch cleanup (local `git branch -D`, remote `--delete-branch`, periodic audit)
- **Rule 64**: Pre-existing CI failure diagnosis — reproduce on master before blocking PR
- **Rule 65**: Skill asset build isolation — `//go:build ignore` on `agent/skills/*/assets/examples/*.go`
- **Rule 66**: No parallel agent work — post STOP comment before manual intervention
- **Rule 67**: Agent instructions in PR comments, not PR body
- **Rule 68**: Jules PR detection — check first comment for "PR created automatically by Jules", add `jules` label
- **Rule 69**: Jules PR reviewer protocol — comments to Jules MUST come from `alsotoes`, not `github-actions[bot]`
- **Rule 70**: Steering rule → reviewer script sync — update `ai_reviewer.py` when adding rules that affect PR review/labeling/commenting/merge

## Pre-Flight Checklist

Before starting any autonomous work, verify:

1. **Branch**: `git branch --show-current` shows `master`
2. **Master is up to date**: `git pull` reports "Already up to date"
3. **No uncommitted changes**: `git status` shows clean working tree
4. **GitHub CLI authenticated**: `gh auth status` shows active account

## Per-Task Cycle (15 Steps)

For each task (bug fix or feature), execute these steps strictly sequentially. Do NOT start the next task until the current one is merged.

### Step 1: Create GitHub Issue
```bash
gh issue create \
  --title "<type>: <description>" \
  --label "<bug|enhancement>" \
  --assignee "alsotoes" \
  --body "<issue documentation>"
```
Record the issue number (`ISSUE_N`).

**Issue type guidance:**
- **Bug fix**: `--title "Bug: ..."`, `--label "bug"`
- **New feature**: `--title "Feature: ..."`, `--label "enhancement"`

### Step 2: Create Branch
```bash
git checkout master && git pull
git checkout -b <type>/<N>-<short-slug>
```
**Branch naming:**
- Bug fix: `fix/bug-<N>-<short-slug>`
- Feature: `feature/<N>-<short-slug>`

### Step 3: Implement + Test
Implement the fix or feature and any necessary tests. Run:
```bash
go build ./...
go test ./...
gofmt -w <modified files>
```

### Step 4: Commit
```bash
git add <files>
git commit -m "<type>: <description> (#ISSUE_N)"
```
**Commit prefix:**
- Bug fix: `fix:`
- Feature: `feat:`

### Step 5: Pre-Push Branch Validation (Rule 58)
**CRITICAL**: Before pushing, verify you are on the correct branch:
```bash
git branch --show-current
```
The output MUST match `<type>/<N>-<short-slug>`. If it doesn't, STOP and re-checkout the correct branch. Never push to `master` directly.

### Step 6: Push
```bash
git push -u origin <type>/<N>-<short-slug>
```

### Step 7: Create PR
```bash
gh pr create \
  --title "<type>: <description>" \
  --body "Resolves #ISSUE_N

<full issue documentation, implementation explanation, changelog>" \
  --base master
```
Record the PR number (`PR_N`).

### Step 8: Assign PR (Rule 51)
```bash
gh pr edit PR_N --add-assignee alsotoes
```

### Step 9: Add Label (Rule 52)
```bash
gh pr edit PR_N --add-label <bug|enhancement>
```

### Step 10: Wait for CI + Reviewer
Poll until all checks complete:
```bash
gh pr checks PR_N
```
Wait until NO checks show `pending`. Then read the reviewer's latest review:
```bash
gh pr view PR_N --json reviews --jq '.reviews[-1].body'
```

### Step 11: Evaluate Reviewer Feedback
- If the review contains `✅`: proceed to Step 14 (merge gate).
- If the review found issues: proceed to Step 12 (address feedback).

### Step 12: Address Reviewer Feedback
1. Fix the code per reviewer findings
2. Run `go build`, `go test`, `gofmt`
3. Commit the fix
4. **Rule 58**: Verify branch before push: `git branch --show-current`
5. Push: `git push`
6. **Rule 53**: Post a visible PR comment explaining what changed:
   ```bash
   gh pr comment PR_N --body "## Update: Reviewer feedback addressed

   ### Changes in commit <hash>
   <what changed and why>"
   ```
   **Rule 69**: If the PR has the `jules` label, this comment MUST be posted from an authenticated `alsotoes` session (PAT), not `github-actions[bot]`. See [Jules PR Detection & Reviewer Protocol](#jules-pr-detection--reviewer-protocol-rules-68-69).
7. **Rule 54**: Update PR body with new changelog entry
8. Go to Step 10 (wait for CI + reviewer again)

### Step 13: AIFS Doubt Protocol (Rule 59)
If at ANY point you encounter something you don't know or understand:
1. STOP work immediately
2. Create a blocking issue:
   ```bash
   gh issue create \
     --title "[AIFS] <question or doubt>" \
     --label "bug" --label "automation" \
     --assignee "alsotoes" \
     --body "Blocked PR #PR_N pending resolution of this question.

   <context and specific question>"
   ```
3. Link the AIFS issue to the PR (add a comment on the PR referencing the AIFS issue)
4. Wait for human input before continuing

### Step 14: Merge Gate (Rule 55)
Verify ALL conditions before merging:
1. All checks pass (including `benchstat`): `gh pr checks PR_N` — none `pending`, none `fail`
2. Reviewer's latest review contains `✅`
3. If both conditions met, merge:
   ```bash
   gh pr merge PR_N --merge --delete-branch
   ```

### Step 15: Post-Merge Cleanup
1. **Rule 56**: Close the issue:
   ```bash
   gh issue close ISSUE_N
   ```
2. **Rule 57**: Return to master:
   ```bash
   git checkout master && git pull
   ```
3. **Rule 63**: Delete local branch:
   ```bash
   git branch -D <branch-name>
   ```
4. **Rule 63**: Periodically audit and delete stale remote branches:
   ```bash
   git push origin --delete <stale-branch>
   ```
5. Proceed to the next task.

## Manual Intervention & Circuit Breaker (Rules 14, 18)

When the **3-push circuit breaker** trips (an automated agent has pushed 3 times without reaching All-Green), the automated loop MUST halt. A human (or a human-directed agent like opencode) must then take over manually.

### Manual Intervention Procedure

1. **Post a STOP comment on the PR** — explicitly instruct all automated agents (Jules, etc.) to cease work:
   ```bash
   gh pr comment PR_N --body "## ⛔ Manual Intervention — Automated Loop Halted

   Per Rule 14 & 18 (3-Push Circuit Breaker), the automated agent loop has reached
   its maximum iterations. @google-labs-jules, please STOP all work on this PR.
   No further automated commits or pushes.

   @alsotoes is now performing a manual final review."
   ```
   **Rule 69**: If the PR has the `jules` label, this STOP comment MUST be posted from an authenticated `alsotoes` session (PAT). Jules ignores comments from `github-actions[bot]`.

2. **Pull the latest PR state and code**:
   ```bash
   gh pr view PR_N --json commits,reviews,comments,statusCheckRollup
   git fetch origin && git checkout <pr-branch>
   ```

3. **Detect Jules PR (Rule 68)**: Check if the PR's first comment contains "PR created automatically by Jules". If so, add the `jules` label and follow the Jules Reviewer Protocol (Rule 69) for all subsequent comments:
   ```bash
   gh pr edit PR_N --add-label jules
   ```

4. **Review all commits and reviewer feedback** — read every review pass to understand what was fixed and what remains.

5. **Verify the code locally**:
   ```bash
   go build ./...
   go vet ./...
   go test ./...
   ```

6. **Fix any remaining issues directly** (see "Handling Pre-Existing CI Failures" below).

7. **Merge master into the PR branch** (Rule 50) before pushing any manual fixes:
   ```bash
   git merge master --no-edit
   ```

8. **Commit, push, comment, and update PR body** (Rules 53, 54) — same as the normal cycle. **Rule 69**: If the PR has the `jules` label, comments MUST be from `alsotoes`.

9. **Wait for all CI checks** — note that `benchstat` can take **7+ minutes** to complete.

10. **Merge once All-Green** (Rule 55), close issue (Rule 56), clean up (Rules 57, 63).

### Critical: No Parallel Work (Rule 66)

When manual intervention is in progress, **no automated agent may push to the PR branch**. The STOP comment must be posted *before* any manual work begins. This prevents:
- Conflicting commits on the same files
- Force-push wars between agents
- Race conditions on CI runs

## Handling Pre-Existing CI Failures (Rule 64)

A PR may have failing CI checks that are **pre-existing on master** and unrelated to the PR's changes. These must be handled correctly:

### Diagnosis Procedure

1. **Identify the failing check** and read its logs:
   ```bash
   gh run view <run-id> --log-failed
   ```

2. **Reproduce on master** to confirm it's pre-existing:
   ```bash
   git checkout master
   # Run the failing check's command locally
   git checkout <pr-branch>
   ```

3. **If pre-existing**: fix it as part of the manual intervention. Do NOT block the PR on unrelated failures.

4. **If introduced by the PR**: fix the PR code that caused the failure.

### Example: Vendoring Parity (Rules 25, 65)

Go files in `agent/skills/*/assets/examples/` that import external packages not in `go.mod` will break `go work vendor`. These are skill reference files, not project code.

**Fix**: Add `//go:build ignore` build tag to exclude them from compilation and vendoring while keeping them as readable reference material:
```bash
for f in agent/skills/*/assets/examples/*.go; do
  # Prepend: //go:build ignore\n\n
done
```
Verify: `go work vendor` exits 0 and `git status --porcelain vendor/` shows no changes.

## Duplicate Tracking Issues (Rule 20)

The auto-reviewer may autonomously create tracking issues for PRs. Sometimes **duplicates** are created (e.g., two issues for the same PR). The agent MUST:

1. **Search before creating**:
   ```bash
   gh issue list --state open --search "<PR title or number>"
   ```

2. **If duplicates exist**: close all but one with an explanatory comment:
   ```bash
   gh issue close <dup-issue> --reason "not planned" \
     --comment "Closing as duplicate of #<canonical-issue> (Rule 20)."
   ```

3. **Reuse the canonical issue**: update its body with actionable remaining work rather than creating a new one.

4. **Link the PR**: ensure the PR description contains `Resolves #<canonical-issue>`.

## PR Comments vs PR Body for Agent Instructions (Rule 67)

**Instructions to other agents (Jules, etc.) must be posted as PR comments, NOT embedded in the PR description.**

- **PR body**: `Resolves #ISSUE_ID`, high-level description, changelog, reviewer status section.
- **PR comments**: actionable instructions to other agents, status updates, review responses.

Rationale: PR comments appear in the conversation timeline and trigger notifications. PR body edits are silent and may be missed by automated agents watching for new comments.

## Jules PR Detection & Reviewer Protocol (Rules 68, 69)

### Detecting Jules-Created PRs (Rule 68)

Jules-created PRs are identified by either of these phrases in the PR's first comment or PR body:
- **"PR created automatically by Jules"**
- **"Jules, reporting for duty"**

When either phrase is detected, the agent MUST immediately add the `jules` label:

```bash
gh pr edit PR_N --add-label jules
```

This detection MUST be performed **before** any reviewer interaction or feedback addressing begins. The `jules` label selects the Jules-specific reviewer protocol (Rule 69) for all subsequent operations on that PR.

### Jules Reviewer Protocol (Rule 69)

When a PR has the `jules` label, **all comments directed at the Jules agent MUST be posted by `alsotoes`** (using a PAT), NOT by `github-actions[bot]`. The Jules agent only recognizes and acts upon comments from `alsotoes`; comments from the bot account are silently ignored.

This affects:

| Action | Normal PR | Jules-labeled PR |
|--------|-----------|------------------|
| Reviewer feedback (Gemini) | `github-actions[bot]` | `github-actions[bot]` (read-only, no change) |
| Feedback addressing (Rule 53) | Any authenticated user | **`alsotoes`** via PAT |
| STOP comments (Rule 66) | Any authenticated user | **`alsotoes`** via PAT |
| Merge status updates | Any authenticated user | **`alsotoes`** via PAT |
| Actionable instructions to Jules | Any authenticated user | **`alsotoes`** via PAT |

**Procedure**: The Gemini reviewer's review (posted as `github-actions[bot]`) is still read by the human/agent to determine if issues exist. However, any follow-up instructions to Jules MUST be re-posted by `alsotoes`:

```bash
# Read the Gemini reviewer's feedback (bot identity is fine for reading)
gh pr view PR_N --json reviews --jq '.reviews[-1].body'

# Post follow-up instructions to Jules — MUST be from alsotoes
gh pr comment PR_N --body "## Reviewer Feedback for @google-labs-jules

The Gemini reviewer found the following issues:
<summarize findings>

Please address these and push updates."
```

**Critical**: The `gh pr comment` command MUST be run from an authenticated `alsotoes` session (PAT), not from the `GITHUB_TOKEN` used by CI. Comments posted as `github-actions[bot]` will NOT be actioned by Jules.

## Common Pitfalls & Solutions

### Forgetting Labels and Assignment (Rules 49, 51, 52)
**Pitfall**: During manual intervention, it's easy to forget assigning the PR and adding labels.
**Solution**: Always run these immediately after creating or taking over a PR:
```bash
gh pr edit PR_N --add-assignee alsotoes
gh pr edit PR_N --add-label bug        # for bug fixes
gh pr edit PR_N --add-label enhancement # for features
gh pr edit PR_N --add-label automation  # for AI-driven work
```

### Jules PR Comments Posted as Bot (Rule 69)
**Pitfall**: Posting reviewer feedback or STOP comments as `github-actions[bot]` on a Jules-created PR. Jules only recognizes comments from `alsotoes` and will silently ignore bot comments, causing Jules to continue working or miss feedback.
**Solution**: Before posting any comment on a PR, check for the `jules` label. If present, ensure the `gh pr comment` command runs from an authenticated `alsotoes` session (PAT), not the `GITHUB_TOKEN` used by CI:
```bash
# Check for jules label
gh pr view PR_N --json labels --jq '.labels[].name'

# If "jules" is present, comment as alsotoes (PAT session)
gh pr comment PR_N --body "..."
```

### Forgetting to Update the Reviewer Script (Rule 70)
**Pitfall**: Adding or modifying a steering rule that affects PR review, labeling, commenting, or merge behavior, but forgetting to update `.github/scripts/ai_reviewer.py` and `.github/workflows/gemini_reviewer.yml` to implement the new behavior. The rule exists on paper but is never enforced in code.
**Solution**: Before merging a PR that adds or modifies rules in `openspec/config.yaml`, verify that the reviewer script implements the new rule. If the rule affects any of these areas, the PR MUST include corresponding changes to `ai_reviewer.py`:
- PR detection logic (e.g., Jules detection per Rule 68)
- Labeling (Rules 49, 52, 68)
- Comment posting identity — bot vs PAT (Rules 48, 69)
- Circuit breaker enforcement (Rules 14, 18)
- Traceability checks (Rules 11, 20)
- Merge gate conditions (Rule 55)

If no script change is needed, add a PR comment explaining why.

### Pre-Commit Hooks Updating Benchmark Docs
**Pitfall**: The pre-commit hook regenerates `docs/PERFORMANCE.md` and `.github/data/benchmark_history.csv`, adding unexpected files to the commit.
**Solution**: This is expected behavior. Include these files in the commit. Do NOT revert them. Per Rule 61, when rebasing, resolve these by taking the master version (`--theirs`) since they are regenerated.

### Benchstat Check Timing
**Pitfall**: The `benchstat` CI check can take **7+ minutes** to complete, causing premature merge attempts.
**Solution**: Budget at least 8 minutes for the final CI wait. Poll with `gh pr checks PR_N` until NO checks show `pending`.

### Stale Branch Missing Master Files
**Pitfall**: A PR branch created from an older master may be missing files that exist on current master. CI runs on the **merge commit** (PR branch + master), so failures from missing files appear even though the PR branch doesn't have them.
**Solution**: Always merge master into the PR branch before pushing (Rule 50):
```bash
git merge master --no-edit
```

## Key Principles

### Never Assume (Rule 59)
The project works with many tools (Gemini, Jules, opencode, etc.). Between operations:
- The branch may have changed
- Master may have new commits
- Files may have been modified
- PRs may have been closed or merged by other agents

Always verify state before acting. When in doubt, stop and create an `[AIFS]` issue.

### Post-Push Visibility (Rules 53, 54)
After EVERY push to a PR branch:
1. Post a visible comment on the PR (`gh pr comment`) explaining what changed
2. Update the PR body with a new changelog entry

Silent pushes are prohibited. The reviewer and collaborators must always know what changed and why.

### Strictly Sequential
One task at a time. Wait for merge before starting the next. This prevents merge conflicts and keeps the review cycle clean.

### No Parallel Work on the Same PR (Rule 66)

When manual intervention is happening, automated agents MUST be told to STOP. No two agents (human-directed or autonomous) should push to the same PR branch simultaneously. This prevents conflicting commits, force-push wars, and CI race conditions.

## Flow Diagram

```
[Start]
  │
  ├─ 1. Create issue (label: bug|enhancement, assignee: alsotoes)
  ├─ 2. Create branch off master (fix/ or feature/)
  ├─ 3. Implement + tests
  ├─ 4. Commit (fix: or feat:)
  ├─ 5. Validate branch (Rule 58)
  ├─ 6. Push
  ├─ 7. Create PR (Resolves #NNN)
  ├─ 8. Assign PR to alsotoes (Rule 51)
  ├─ 9. Add label bug|enhancement (Rule 52)
  ├─ 10. Wait for CI + reviewer
  │      │
  │      ├─ Reviewer found issues?
  │      │   ├─ YES → 12. Fix → push → comment (Rule 53) → update body (Rule 54) → back to 10
  │      │   └─ NO (✅) → continue
  │      │
  │      ├─ Something unclear? → 13. AIFS issue → wait for human
  │      │
  │      └─ Circuit breaker tripped (Rule 14/18)? → MANUAL INTERVENTION
  │            │
  │            ├─ Post STOP comment to PR (halt automated agents)
  │            ├─ Pull latest PR state + code
  │            ├─ Review all commits + reviewer feedback
  │            ├─ Verify code locally (build, vet, test)
  │            ├─ Fix remaining issues directly
  │            ├─ Merge master into branch (Rule 50)
  │            ├─ Commit → push → comment (Rule 53) → update body (Rule 54)
  │            ├─ Wait for all CI (benchstat can take 7+ min)
  │            └─ Continue to merge gate (Step 14)
  │
  ├─ 14. All checks pass + reviewer ✅? → merge
  ├─ 15. Close issue + return to master + cleanup branches (Rule 63)
  │
  └─ [Next task]
```
