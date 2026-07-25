# AGENTS.md — Steering Rules for AI Agents

## Bug-Fix PR Workflow (Strictly Sequential)

When fixing bugs via the GitHub issue + PR + reviewer cycle, these rules are MANDATORY for every bug. No exceptions.

### Rule A: Assign PR to alsotoes
After creating a PR, immediately assign it to `alsotoes`:
```
gh pr edit <PR_NUMBER> --add-assignee alsotoes
```

### Rule B: Add `bug` label to PR
After creating a PR, immediately add the `bug` label:
```
gh pr edit <PR_NUMBER> --add-label bug
```

### Rule C: Update PR body after EVERY push
After every `git push` to a PR branch, update the PR body with a new changelog entry describing what changed in that commit. The PR body must always contain:
1. `Fixes #<issue_number>` link
2. Bug description (severity, category, location)
3. Problem explanation
4. Fix explanation
5. **Changelog** section with one entry per commit (hash, description, what changed and why)
6. Reviewer status section (updated after each review)
7. Testing section

### Rule D: Wait for ALL checks before merging
Do NOT merge until ALL checks pass (including benchstat). Use:
```
gh pr checks <PR_NUMBER>
```
Confirm every check shows `pass` (none `pending`, none `fail`).

### Rule E: Reviewer green light required
Do NOT merge until the Gemini reviewer's latest review contains `✅`. If the reviewer found issues, address them, commit, push, update PR body (Rule C), and re-wait.

### Rule F: Close issue after merge
After merging a PR, close the linked issue:
```
gh issue close <ISSUE_NUMBER>
```

### Rule G: Return to master after merge
After merging, checkout master and pull:
```
git checkout master && git pull
```

### Full per-bug cycle:
1. Create GitHub issue (label: bug, assignee: alsotoes)
2. Create branch `fix/bug-N-<slug>` off master
3. Write fix + tests
4. Commit + push
5. Create PR (body: Fixes #NNN + full bug documentation)
6. **Rule A**: `gh pr edit <PR> --add-assignee alsotoes`
7. **Rule B**: `gh pr edit <PR> --add-label bug`
8. **Rule C**: Update PR body with changelog
9. Wait for CI + reviewer (poll `gh pr checks`)
10. Read reviewer review (`gh pr view <PR> --json reviews`)
11. If reviewer found issues → fix → commit → push → **Rule C** (update PR body) → go to step 9
12. If reviewer ✅ AND all checks pass (**Rule D**, **Rule E**) → merge PR
13. **Rule F**: Close issue
14. **Rule G**: Return to master
15. Next bug
