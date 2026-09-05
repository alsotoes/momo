# AI Flying Solo — Autonomous Development Workflow

This document defines the complete workflow for AI agents working autonomously on bug fixes **and** new features without direct human supervision. All AI agents MUST follow this guide when operating in "Flying Solo" mode.

## Steering Rules Reference

This document is governed by the steering rules in [`openspec/config.yaml`](../openspec/config.yaml) (single source of truth, Rule 39). The following rules are specifically relevant:

- **Rules 14, 18**: 3-push circuit breaker — automated agent loop halts after 3 failed iterations; manual intervention required
- **Rule 20**: Autonomous traceability — reuse existing tracking issues, never create duplicates
- **Rule 25**: Workspace vendoring parity — `go work vendor` must produce no diff
- **Rules 49, 51, 52**: Issue/PR assignment and labeling (assignee: alsotoes, labels: bug|enhancement + automation)
- **Rule 72**: Issue Ownership Gate — assign the issue to alsotoes and validate labels BEFORE starting work
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
- **Rule 71**: Master CI gate before new work — wait for all master CI workflows to finish and pass before creating a new branch
- **Rule 72**: Issue Ownership Gate — assign the issue to `alsotoes`, ensure `bug`/`enhancement` + `automation` labels, before starting any work
- **Rule 73**: Spec-First Implementation Mandate — every new feature / spec-driven change MUST author an OpenSpec change (`openspec/changes/<id>/`) linked to a GitHub issue BEFORE implementation; the PR MUST include the spec and `Resolves #ISSUE_ID`. Bug fixes are exempt from a formal spec but still need a tracking issue
- **Rule 74**: Seam-Over-Plugins — adaptive/mutating behavior (degraded-read, self-heal/rebuild, R4 momofs FS semantics) MUST be a compile-time Go interface seam (constructor/functional-option injection + compiled-in registry, selected by declarative policy); external dynamic plugins forbidden in the data path (read-only policy feeds only); fast paths concrete/zero-indirect; core trust invariants stay in the auditable core. See `docs/momofs/PLUGIN_ARCHITECTURE.md`
- **Rule 75**: No Networked pprof on Unauthenticated Listeners — profiling via file-based `go test -cpuprofile/-memprofile` flags only; no `net/http/pprof` listener on the unauthenticated data path (RCE-class surface); future admin endpoint = loopback/Unix socket only, boot-enabled, TLS if it leaves loopback
- **Rule 76**: Blog Post Per Ratified Change — every ratified feature/enhancement OpenSpec change MUST ship a Hugo-format post in `docs/blog/posts/` (same PR or immediately-following) with date=anchor issue/PR `createdAt` (or earliest code/plan commit), implemented-state grounding (no `docs/momofs/` design presented as shipped), ⚡ Bolt/🛡 Sentinel tags where relevant, plus `artifacts`/`related` front matter. `no-blog` justification exempts internal-only changes. Reviewer enforces (Rule 70)
- **Rule 77**: ADR Mandate — every ratified feature/enhancement OpenSpec change MUST have an ADR in `docs/adr/NNNN-<change-id>.md` following `docs/adr/template.md`, linking spec + issue + PR + blog post; status `Proposed` → `Accepted` on merge; supersession = new ADR + `Deprecated` old (Fowler)
- **Rule 78**: ADR-Spec Synchronization — ADRs are auto-generated from specs via `make adr-sync` (context ← proposal, decision ← requirement summaries, status ← tasks.md checkboxes, blog link ← issue match); `make adr-sync-check` validates parity and runs in CI; ADR status never hand-edited
- **Rule 79**: No Direct Push to Master — all changes go through issue → branch → PR (`Resolves #ISSUE_ID`) → CI → reviewer → merge `--merge --delete-branch`. Doc-only / pre-commit-regen / trivial-refactor / CI-fix / emergency-hotfix exceptions may bypass, but must be tagged in the commit message
- **Rule 89**: MemPalace Knowledge Grounding — run `mempalace mine .` from repo root + `npx repomix` to refresh context, then query `mempalace search "<topic>" --wing momo` BEFORE reading source files / re-investigating architecture. Reuse indexed project knowledge instead of re-loading files to cut token burn.
- **Rule 90**: Auto-Trace Issue Deduplication — the AI reviewer MUST NEVER create duplicate auto-trace issues for the same PR: search existing OPEN auto-trace issues first and reuse the canonical, use the CURRENT PR body from the API (not the stale event payload), and rely on workflow `concurrency` (`cancel-in-progress: true`) to serialize pushes. Prevents the #1057 duplicate-issue incident.

## Pre-Flight Checklist

Before starting any autonomous work, verify:

1. **Branch**: `git branch --show-current` shows `master`
2. **Master is up to date**: `git pull` reports "Already up to date"
3. **No uncommitted changes**: `git status` shows clean working tree
4. **GitHub CLI authenticated**: `gh auth status` shows active account
5. **Master CI is all-green (Rule 71)**: All CI workflows on `master` must be complete and passing before creating a new branch:
   ```bash
   gh run list --branch master --limit 5
   ```
   - If any run shows `in_progress` or `queued`: **WAIT**. Poll every 60 seconds until all runs complete.
   - If any run shows `failure`: **STOP**. Diagnose and fix the failure on `master` (per Rule 64) before starting new work.
   - Only proceed when all runs show `completed` with `success` conclusion.
   - **Rationale**: After merging a PR, GitHub Actions re-runs all workflows on `master`. Branching from mid-CI `master` risks branching from code that may fail or be reverted. This gate guarantees every new branch originates from a fully validated, stable `master`.
6. **MemPalace knowledge base is current (Rule 89)**: Refresh the project knowledge base so prior decisions, specs, and architecture are queryable instead of re-derived (token efficiency):
   ```bash
   # Mine the repo into MemPalace (wing "momo")
   mempalace mine . --wing momo
   # Regenerate the codebase context map when the code has changed
   npx repomix    # regenerates repomix-output.xml
   ```
   - Re-mine when significant code/spec/doc changes landed since the last session.
   - After mining, **query MemPalace for grounding BEFORE reading source files** (see Step 1c).

## Per-Task Cycle (18 Steps)

For each task (bug fix or feature), execute these steps strictly sequentially. Do NOT start the next task until the current one is merged.

### Step 0: Issue Ownership Gate (Rule 72)

**Before ANY work on an issue — whether newly created, pre-existing, or picked up from a batch — establish ownership and validate the issue's metadata.**

```bash
# 1. Assign the issue to the maintainer
gh issue edit ISSUE_N --add-assignee alsotoes

# 2. Validate labels: category (bug|enhancement) + automation
gh issue view ISSUE_N --json assignees,labels

# 3. Add any missing labels
gh issue edit ISSUE_N --add-label <bug|enhancement>
gh issue edit ISSUE_N --add-label automation
```

Only proceed to implementation (Step 2) once the issue is assigned to `alsotoes` and carries both the category label and the `automation` label. Issues lacking an assignee or the `automation` label are orphaned and MUST be remediated before work begins.

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

### Step 1b: Author OpenSpec Change (features) — Rule 73

**ADR Creation (Rules 77/78):** After the OpenSpec change is authored, run `make adr-sync` to auto-generate the ADR in `docs/adr/NNNN-<change-id>.md` from the spec (context ← proposal.md, decision ← spec requirement summaries, status ← tasks.md checkboxes). The ADR links spec + issue + PR + blog post. Never hand-edit an ADR — regenerate it. `make adr-sync-check` (CI) validates parity. ADR status `Proposed` → `Accepted` on merge; supersession = new ADR + `Deprecated` old (Fowler).

**MemPalace knowledge store (Rule 89):** The project knowledge base lives in MemPalace under `wing: momo`. Refresh it with `mempalace mine . --wing momo` from the repo root, and regenerate the codebase map with `npx repomix` when the code changed. **Before reading source files or re-investigating architecture, query MemPalace first** — it is the durable memory layer reusing indexed specs, decisions, and code patterns across sessions, cutting token burn:
```bash
mempalace search "<topic>" --wing momo [--room <room>] [--results N]
```
Only fall back to full file reads / `repomix-output.xml` inspection when MemPalace search returns insufficient grounding. Keep it current by re-mining `src/`, `docs/`, `openspec/`, `conf/`, `tools/` after significant changes.

**Blog post (Rule 76):** Every feature PR MUST ship a matching blog post under `docs/blog/posts/NNN-<slug>.md` (front matter per `docs/blog/README.md`; `date` = anchor issue/PR `createdAt`), OR carry an explicit `no-blog` justification (ADR sibling `<adr>.no-blog.md` for internal-only changes). Coverage is enforced by `make blog-check` in CI, not just by the reviewer.

For **ANY new feature / spec-driven change / architectural shift** (NOT routine bug fixes), author the OpenSpec change proposal on the branch BEFORE implementing:

```bash
mkdir -p openspec/changes/<change-id>/specs/<change-id>
```

Create three files (mirror the existing `openspec/changes/*/` layout):
- `proposal.md` — title `# Change: <title>`, `**Related Issues:**` linking `https://github.com/alsotoes/momo/issues/<ISSUE_N>`, then `## Why`, `## What Changes`, `## Non-Goals`.
- `specs/<change-id>/spec.md` — **first line preserves Rule 11 linkage**: `> GitHub Issue URL: https://github.com/alsotoes/momo/issues/<ISSUE_N>`, then `## Purpose` and Requirement/Scenario blocks (`### Requirement` + `#### Scenario` Gherkin).
- `tasks.md` — phased implementation checklist.

**Rules:**
- The spec `spec.md` MUST link the GitHub issue at the top (Rule 11).
- The feature PR (Step 7) MUST include the OpenSpec change files and its body MUST use `Resolves #ISSUE_N`.
- Author the spec on the branch in this step; implement in Step 3; both ship in the same PR.
- **Bug fixes / internal refactors with no behavioral surface are exempt** from a formal spec (Rule 73) but MUST still have a tracking issue from Step 1.

### Step 1c: Ground via MemPalace (Rule 89)

Before implementing, query the project knowledge base to reuse indexed context instead of re-reading files (token efficiency):

```bash
# Refresh the knowledge base if the codebase has changed since the last mine
mempalace mine . --wing momo

# Ground the task: search for relevant prior specs, decisions, and code patterns
mempalace search "<task topic>" --wing momo [--room <room>] [--results N]
```

Guidelines:
- Search for the task's domain (e.g., `replication splay`, `metadata quorum`, `scrub integrity`, `FUSE mount`) and for the files/features the task touches.
- When the task is a takeover or follow-up (Jules PR, prior phase), search for the prior work to recover decisions and constraints.
- **Only read source files / `repomix-output.xml` when MemPalace returns insufficient grounding.** Prefer the indexed knowledge first.
- Keep the base current: re-mine after significant code/spec/doc changes (Pre-Flight item 6).
- **Auto-Trace check (Rule 90):** When taking over a PR or observing auto-trace issues, verify no duplicate auto-trace issues exist for the PR before creating or re-creating any. Consolidate to the canonical issue (Rule 80) and ensure the PR body carries `Resolves #<canonical>` so the reviewer short-circuits (its `find_existing_auto_trace` + `get_current_pr_body` prevent duplicate creation; see Rule 90).

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

**Rule 74 (adaptive/mutating behavior):** if the change is an adaptive or
mutating behavior (e.g., degraded-read, self-heal/rebuild, R4 momofs FS
semantics), implement it as a **compile-time Go interface seam** — interface +
constructor/functional-option injection + compiled-in registry, selected by a
declarative policy — NOT an external dynamic plugin (go-plugin/.so/RPC). Keep
the happy path concrete/zero-indirect; dispatch the seam only at decision
points; never bypass the CAS validate→write chokepoint. See
`docs/momofs/PLUGIN_ARCHITECTURE.md`.

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
   make adr-sync-check   # Rule 78: ADRs match specs
   make blog-check       # Rule 76: posts valid + related graph + Accepted-ADR coverage
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

### Jules PR Takeover/Rebuild Protocol (Rule 79)

When a Jules PR requires significant cleanup (rogue commits, wrong dates, bogus Resolves, regressions):

1. **Analyze**: `gh pr view PR_N --json commits,files` — identify rogue changes.
2. **Rebuild**:
   ```bash
   git checkout -b fix/<N>-<slug> origin/master
   git cherry-pick <legitimate-fix-commit>
   # Fix dates, drop rogue changes, gofmt
   ```
3. **Force-push to PR head**: `git push --force-with-lease origin fix/<N>-<slug>:<jules-branch>`
4. **Update PR body**: Add `Resolves #<canonical>`, full reviewer summary (Rule 54).
5. **Post STOP comment** if Jules pushed concurrently (Rule 66).
6. **Monitor CI + reviewer** on new head.

**Common Jules issues to fix**:
- Rogue `go.mod`/`go.sum` reverts (bazil restore, breaking R4 FUSE migration)
- Future-dated `.jules/` learning entries → correct to PR creation date
- Bogus `Resolves #1` → replace with canonical auto-trace issue
- Missing `jules`/`bug`/`enhancement`/`automation` labels

### Auto-Trace Consolidation Protocol (Rule 80)

When multiple auto-trace issues exist for one PR:
1. Identify canonical (lowest number, OPEN).
2. Close dups: `gh issue close <dup1> <dup2> --comment "Duplicate of #<canonical> (canonical tracker for PR #<N>). Closed per Rule 20."`
3. Update PR body: `Resolves #<canonical>`.
4. If new auto-trace created during takeover (stale reviewer read), close it immediately.

**Prevention (Rule 90):** The reviewer script (`ai_reviewer.py`) must not CREATE duplicates: it searches existing OPEN auto-trace issues first (`find_existing_auto_trace`), reuses the canonical, and uses the CURRENT PR body from the API (`get_current_pr_body`) instead of the stale webhook payload. The `gemini_reviewer.yml` workflow carries `concurrency: cancel-in-progress: true` per PR so parallel pushes are serialized. If duplicates still appear, that is a Rule 90 bug — fix the script, don't just close the issues.

### Phase-Based Implementation Pattern (Rule 81)

For large features (estimated >1 PR):
1. **One OpenSpec change**: `openspec/changes/<feature-id>/` with proposal.md, specs/<id>/spec.md, tasks.md.
2. **tasks.md** lists ALL phases with checkboxes; link to GitHub issue.
3. **Phase 0**: Author spec on branch, `make adr-sync`, PR with `Resolves #ISSUE_N` (spec-only, no code).
4. **Phase N**: Separate branch `feature/<N>-<slug>-phaseN`, implement phase tasks, update tasks.md.
5. **Each phase PR**: `Resolves #ISSUE_N` (same issue); `tasks.md` shows phase progress.
6. **Issue stays OPEN** until final phase merges; auto-closes via `Resolves` on last phase.

### Benchmark Flake Allowlist Pattern (Rule 82)

When a benchmark flakes on CI:
1. **Rerun failed job** — if passes, likely noise.
2. **Local comparison**: run bench on HEAD^1 vs HEAD locally — if binary-identical code, it's noise.
3. **Add to `.github/workflows/benchmark_compare.yml` allowlist** with rationale comment matching precedent.
4. **Example entry**:
   ```yaml
   - name: LocalWrite
     # Temp file I/O in /tmp on shared runners; known noise per #846, #955, #960
   - name: S3PutSpool
     # Temp file I/O (os.CreateTemp+io.Copy) in /tmp on shared runners; similar to LocalWrite
   ```

### PR Rejection Protocol (Rule 83)

When a PR is fundamentally incorrect (regression, wrong fix):
1. Post detailed rejection review on PR with code analysis + empirical proof.
2. Close PR: `gh pr close PR_N --comment "<detailed rationale>"`
3. Close auto-trace issues: `gh issue close <dups> --comment "Duplicate of rejection rationale for PR #<N>. Closed per Rule 20."`
4. Delete branch: `gh api -X DELETE repos/.../git/refs/heads/<branch>`
5. **Do NOT merge** — no salvage via partial fix; the approach is wrong.

### Jules Learning File Date Fix (Rule 84)

Jules `.jules/*.md` entries often have future/incorrect dates. During takeover:
- Correct to PR creation date: `gh pr view PR_N --json createdAt --jq '.createdAt'` → extract date.
- Existing pattern: `2026-08-*` / `2026-09-01` format.
- Update entry date in `.jules/<file>.md` before commit.

### Benchmark Hook Management (Rule 85)

- **Implementation commits**: `git commit --no-verify` (skip hook bench regeneration).
- **Benchmark addition commits**: Let hook run (adds new rows).
- **Chore commits**: `chore: regenerate bench docs (pre-commit hook)` for hook output.

### Direct-Master Doc Commits (Rule 86)

Trivial doc fixes (typos, clarifications, link fixes) MAY commit directly to master:
```bash
git commit -m "docs: <description> (#ISSUE_N)" --no-verify
```
Criteria: No code changes, no behavioral surface, pure markdown/typos/links.

### Reviewer Re-Evaluation Trigger (Rule 87)

AI reviewer only re-evaluates on `synchronize` (push). To force re-eval:
- `git commit --allow-empty -m "sync: trigger reviewer re-evaluation" && git push`
- Or use PR "Update branch" button.

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

### Forgetting the Issue Ownership Gate (Rule 72)
**Pitfall**: Starting implementation on a pre-existing or batch issue that is unassigned or missing the `automation` label (e.g., the #606–#623 batch, where all issues lacked an assignee).
**Solution**: Before any work, run the Issue Ownership Gate (Step 0): assign the issue to `alsotoes` and validate/add the category + `automation` labels via `gh issue edit ISSUE_N`. Treat missing assignee/labels as a blocking condition.

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

### Benchstat Check Timing & Frozen Check Status
**Pitfall**: The `benchstat` CI check can take **7+ minutes** to complete, and `gh pr checks` may report a check (e.g., `benchstat`) as `pending` for 15+ minutes with a frozen `updatedAt` while the underlying job is actually `completed/success`.
**Solution**: Budget at least 8 minutes for the final CI wait. Poll with `gh pr checks PR_N` until NO checks show `pending`. If a long-pending check has a frozen `updatedAt`, do NOT deadlock waiting — cross-check the actual job state via the API and read its steps' conclusions:
```bash
gh api repos/<owner>/<repo>/actions/runs/<run-id>/jobs \
  --jq '.jobs[0] | {status, conclusion, steps:[.steps[]|{name,status,conclusion}]}'
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

### Benchstat Check Timing & Frozen Check Status
**Pitfall**: The `benchstat` CI check can take **7+ minutes** to complete, and `gh pr checks` may report a check (e.g., `benchstat`) as `pending` for 15+ minutes with a frozen `updatedAt` while the underlying job is actually `completed/success`.
**Solution**: Budget at least 8 minutes for the final CI wait. Poll with `gh pr checks PR_N` until NO checks show `pending`. If a long-pending check has a frozen `updatedAt`, do NOT deadlock waiting — cross-check the actual job state via the API and read its steps' conclusions:
```bash
gh api repos/<owner>/<repo>/actions/runs/<run-id>/jobs \
  --jq '.jobs[0] | {status, conclusion, steps:[.steps[]|{name,status,conclusion}]}'
```

### Merge-Inflation: Three-Dot vs Two-Dot Diff (Rule 62/55/73)
**Pitfall**: After Rule 50 merges `master` into an external PR branch, two-dot diff (`gh pr view --json files`, `gh pr diff --name-only`) inflates to include ALL of master's merged files as if they were PR-authored. The reviewer can then flag master files (e.g., `checksum.go`, `metrics_exporter.go`, `openspec/*`) as Rule 47 deletions or Rule 73 violations that were never introduced by the PR.
**Solution**: To enumerate a PR's *own* changes, always use the three-dot diff (compares against the shared merge-base, showing only the PR's commits):
```bash
git diff master...HEAD --name-only
```
Base every decision on what this PR *actually* changed — merge qualification (Rule 55), stale-review detection (Rule 62), and spec scope (Rule 73) — on this three-dot output, not on `gh pr diff`.

### Stale Branch Missing Master Files
**Pitfall**: A PR branch created from an older master may be missing files that exist on current master. CI runs on the **merge commit** (PR branch + master), so failures from missing files appear even though the PR branch doesn't have them.
**Solution**: Always merge master into the PR branch before pushing (Rule 50):
```bash
git merge master --no-edit
```

### Branching Before Master CI Finishes (Rule 71)
**Pitfall**: After merging a PR, the agent immediately starts the next task and branches from `master`. But GitHub Actions is still running CI on the new `master` commit. If a CI check fails, `master` is in a broken state, and the new branch inherits the broken code.
**Solution**: Before creating any new branch, check that all `master` CI runs are complete and green:
```bash
gh run list --branch master --limit 5
```
If any runs are `in_progress` or `queued`, wait. If any failed, fix `master` first. Only branch from a fully validated `master`.

### Jules PR Takeover Without Rebuild (Rule 79)
**Pitfall**: Taking over a Jules PR and pushing incremental fixes instead of a clean rebuild. Jules PRs often contain rogue commits (go.mod/go.sum reverts, bazil restore, future-dated learning entries, bogus Resolves). Incremental fixes compound the problems; the reviewer sees merge-inflated diffs and spurious violations.
**Solution**: Always do a clean rebuild on current master: `git checkout -b fix/<N>-<slug> origin/master`, cherry-pick only the legitimate fix, fix dates, drop rogue changes, then force-push the clean rebuild to the PR head.

### Auto-Trace Proliferation Without Consolidation (Rule 80)
**Pitfall**: Multiple auto-trace issues created for the same PR (e.g., #982/#983 for #981, #977/#978/#979 for #976). Each auto-trace clutters the tracker and the PR body lacks a proper `Resolves`.
**Solution**: Immediately consolidate to one canonical issue (lowest number, OPEN), close dups with `gh issue close <dups> --comment "Duplicate of #<canonical> (canonical tracker for PR #<N>). Closed per Rule 20."`, update PR body with `Resolves #<canonical>`. If new auto-trace appears during takeover (stale reviewer read), close it immediately.

**Automated-duplicate variant (Rule 90):** When the AI reviewer itself creates 50+ duplicate auto-trace issues for one PR (e.g., #997–#1054 for PR #996), the root cause is the reviewer script — no dedup search, stale event payload, and no workflow concurrency. Fix `ai_reviewer.py` (`find_existing_auto_trace` + `get_current_pr_body`) and add `concurrency: cancel-in-progress: true` to `gemini_reviewer.yml`; then bulk-close the duplicates with the canonical message. Do NOT just close duplicates and leave the script broken — the next synchronize will re-create them.

### Monolithic Feature in One PR (Rule 81)
**Pitfall**: Attempting to implement a large feature (e.g., R6 metadata HA) in a single massive PR. The diff is unreadable, reviewer cannot verify, CI timeout risk, all-or-nothing merge risk.
**Solution**: Split into phases with shared OpenSpec change. Phase 0 = spec authoring; Phase N = implementation + test. Each phase = separate PR with `Resolves #ISSUE_N` (same issue). tasks.md tracks phase progress. Issue auto-closes on final phase merge.

### Benchmark Flake Ignored (Rule 82)
**Pitfall**: A benchmark flakes on CI (e.g., `LocalWrite/64MiB`, `S3PutSpool/64MiB`) and the agent either blocks the PR or merges with a false regression. The benchmark involves temp file I/O on shared runners — it's noise, not regression.
**Solution**: Rerun failed job. If passes, confirm noise via local HEAD^1 vs HEAD comparison (binary-identical code). Add to `.github/workflows/benchmark_compare.yml` allowlist with rationale comment matching precedent. Allowlisted benches exceed 5% alloc without failing CI.

### Merging a Regression Instead of Rejecting (Rule 83)
**Pitfall**: A PR introduces a regression (e.g., #985 breaks hierarchical S3 keys) but the agent merges it because tests pass (tests don't cover the regressed case). The "fix" is fundamentally wrong.
**Solution**: Post detailed rejection review with code analysis + empirical proof. Close PR with rationale. Close auto-trace issues. Delete branch. **Do NOT merge** — no salvage via partial fix; the approach is wrong.

### Jules Learning File Future Date (Rule 84)
**Pitfall**: Jules `.jules/*.md` entries use future dates (e.g., `2026-09-10` when today is `2026-09-01`). The learning file is a knowledge base, not a prediction log.
**Solution**: During takeover, correct date to PR creation date: `gh pr view PR_N --json createdAt --jq '.createdAt'` → extract date. Existing pattern: `2026-08-*` / `2026-09-01` format.

### Benchmark Hook Churn in Implementation Commit (Rule 85)
**Pitfall**: An implementation commit includes the pre-commit hook's benchmark regeneration (200+ lines of `PERFORMANCE.md` + `benchmark_history.csv` churn), bloating the diff and hiding the actual change.
**Solution**: For implementation commits, use `git commit --no-verify` to skip the hook. Let the hook run on benchmark-addition commits. For pure implementation commits, `--no-verify` keeps diff clean.

### Trivial Doc Fix via PR Instead of Direct Master (Rule 86)
**Pitfall**: Opening a PR for a typo fix or link correction. Wastes CI time and review slots.
**Solution**: For pure doc fixes (typos, links, clarifications, zero code/behavior): `git commit -m "docs: <description> (#ISSUE_N)" --no-verify` directly on master. Criteria: no code changes, no behavioral surface.

### Reviewer Stale Verdict on Force-Push (Rule 87)
**Pitfall**: After force-pushing a clean rebuild to a PR, the agent assumes the old reviewer verdict (on the old head) is still valid. The reviewer must re-evaluate on the new head.
**Solution**: Force-push triggers `synchronize` → reviewer re-runs. Verify `gh pr checks PR_N` shows `review` as `pass` on the new head. If stuck, push empty sync commit: `git commit --allow-empty -m "sync: trigger reviewer re-evaluation" && git push`.

### Skipping MemPalace Grounding (Rule 89)
**Pitfall**: Starting a task by reading source files / `repomix-output.xml` from scratch, or relying on model training knowledge, instead of querying the project knowledge base. Re-reads the same files, specs, and decisions every session — wasted tokens and possible drift from the actual codebase state.
**Solution**: Before implementation, run `mempalace search "<topic>" --wing momo` (refresh first with `mempalace mine . --wing momo` + `npx repomix` if the code changed). Ground in indexed prior specs/decisions/code patterns first; read files only when MemPalace returns insufficient context. Re-mine after significant changes so the base stays current.

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
   ├─ 0. Pre-Flight: verify master, clean tree, gh auth, master CI all-green (Rule 71)
   │      │
   │      └─ Master CI still running? → WAIT and poll until all complete
   │         Master CI failed? → STOP, diagnose and fix on master (Rule 64)
   │
   ├─ 0.6 MemPalace refresh (Rule 89): mempalace mine . --wing momo + npx repomix
   │
   ├─ 0.5 Issue Ownership Gate (Rule 72): assign issue to alsotoes + validate labels (bug|enhancement, automation)
   │
   ├─ 1. Create issue (label: bug|enhancement, assignee: alsotoes)
   ├─ 1c. Ground via MemPalace (Rule 89): mempalace search "<topic>" --wing momo BEFORE reading files
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
