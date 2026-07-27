# AI Flying Solo — Autonomous Bug-Fix Workflow

This document defines the complete workflow for AI agents working autonomously on bug fixes without direct human supervision. All AI agents MUST follow this guide when operating in "Flying Solo" mode.

## Steering Rules Reference

This document is governed by the steering rules in [`openspec/config.yaml`](../openspec/config.yaml) (single source of truth, Rule 39). The following rules are specifically relevant:

- **Rules 51-57**: Bug-fix PR workflow (assign, label, comment, update, merge, close, return)
- **Rule 58**: Pre-push branch validation
- **Rule 59**: No-assumption doubt protocol (AIFS)
- **Rule 60**: This document is the authoritative workflow guide
- **Rule 61**: Clean rebase via cherry-pick (avoid pre-commit hook spurious commits on force-push)
- **Rule 62**: Stale reviewer re-trigger (comment to re-run Gemini review after push)
- **Rule 63**: Post-merge branch cleanup (local `git branch -D`, remote `--delete-branch`, periodic audit)

## Pre-Flight Checklist

Before starting any bug-fix work, verify:

1. **Branch**: `git branch --show-current` shows `master`
2. **Master is up to date**: `git pull` reports "Already up to date"
3. **No uncommitted changes**: `git status` shows clean working tree
4. **GitHub CLI authenticated**: `gh auth status` shows active account

## Per-Bug Cycle (15 Steps)

For each bug, execute these steps strictly sequentially. Do NOT start the next bug until the current one is merged.

### Step 1: Create GitHub Issue
```bash
gh issue create \
  --title "Bug: <description>" \
  --label "bug" \
  --assignee "alsotoes" \
  --body "<bug documentation>"
```
Record the issue number (`ISSUE_N`).

### Step 2: Create Branch
```bash
git checkout master && git pull
git checkout -b fix/bug-<N>-<short-slug>
```

### Step 3: Write Fix + Tests
Implement the fix and any necessary tests. Run:
```bash
go build ./...
go test ./...
gofmt -w <modified files>
```

### Step 4: Commit
```bash
git add <files>
git commit -m "fix: <description> (#ISSUE_N)"
```

### Step 5: Pre-Push Branch Validation (Rule 58)
**CRITICAL**: Before pushing, verify you are on the correct branch:
```bash
git branch --show-current
```
The output MUST match `fix/bug-<N>-<short-slug>`. If it doesn't, STOP and re-checkout the correct branch. Never push to `master` directly.

### Step 6: Push
```bash
git push -u origin fix/bug-<N>-<short-slug>
```

### Step 7: Create PR
```bash
gh pr create \
  --title "fix: <description>" \
  --body "Fixes #ISSUE_N

<full bug documentation, fix explanation, changelog>" \
  --base master
```
Record the PR number (`PR_N`).

### Step 8: Assign PR (Rule 51)
```bash
gh pr edit PR_N --add-assignee alsotoes
```

### Step 9: Add Bug Label (Rule 52)
```bash
gh pr edit PR_N --add-label bug
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
5. Proceed to the next bug.

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
One bug at a time. Wait for merge before starting the next. This prevents merge conflicts and keeps the review cycle clean.

## Flow Diagram

```
[Start]
  │
  ├─ 1. Create issue (label: bug, assignee: alsotoes)
  ├─ 2. Create branch off master
  ├─ 3. Write fix + tests
  ├─ 4. Commit
  ├─ 5. Validate branch (Rule 58)
  ├─ 6. Push
  ├─ 7. Create PR (Fixes #NNN)
  ├─ 8. Assign PR to alsotoes (Rule 51)
  ├─ 9. Add bug label (Rule 52)
  ├─ 10. Wait for CI + reviewer
  │      │
  │      ├─ Reviewer found issues?
  │      │   ├─ YES → 12. Fix → push → comment (Rule 53) → update body (Rule 54) → back to 10
  │      │   └─ NO (✅) → continue
  │      │
  │      └─ Something unclear? → 13. AIFS issue → wait for human
  │
  ├─ 14. All checks pass + reviewer ✅? → merge
  ├─ 15. Close issue + return to master
  │
  └─ [Next bug]
```
