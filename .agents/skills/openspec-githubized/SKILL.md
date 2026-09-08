---
name: openspec-githubized
description: "Use whenever an agent operates on an OpenSpec change lifecycle or artifact, including propose/new change, apply/implement tasks, merge/close, sync specs, continue/update artifacts, validate/status checks, or work under openspec/changes/*. This is the GitHub lifecycle overlay for OpenSpec work: bind GitHub issues, maintain openspec/github.yaml, update the GitHub issue business context/status, keep technical design/tasks in the repo, and publish the narrative mirrors (blog post + ADR) that replace Linear Project Documents after merge."
---

# OpenSpec GitHubized

Use this skill to run an OpenSpec change lifecycle while preserving a clear handoff boundary:

- The GitHub issue owns the business "what": business goal, use cases, personas/workflows, scope, acceptance criteria, and stakeholder-facing status. Keep its description synchronized from the business-facing content in `proposal.md`.
- The repo owns the technical "how": OpenSpec design decisions, tasks, implementation details, migrations, tests, and code.
- Canonical specs live under `openspec/specs/`. Unlike the Linear flow (which mirrored specs into Linear Project Documents after archive), the GitHub flow publishes **narrative mirrors at merge time**: the ADR (auto-generated via `make adr-sync`, Rules 77/78) and the blog post (Rule 76) — both shipped in the same PR as the change.

GitHub updates are best effort once project setup exists.

## Invocation Contract

- If the user asks to operate on an OpenSpec change, use this skill as the GitHub lifecycle overlay.
- This skill is the OpenSpec slice of the project's end-to-end `docs/AI_FLYING_SOLO.md` workflow (issue → branch → OpenSpec change → PR → CI → reviewer → merge). Use this skill **in addition to** that workflow, not as a replacement for it.
- Also use the base OpenSpec skill that matches the request, such as `openspec-propose`, `openspec-apply-change`, `openspec-archive-change`, or `openspec-sync-specs`.
- If `openspec/github.yaml` exists, run the relevant GitHub hook for the current phase when possible.
- If `openspec/github.yaml` is missing during proposal/setup, create it before issue binding or other GitHub lifecycle work.
- If `openspec/github.yaml` is missing outside proposal/setup, no-op the GitHub hook and continue normal OpenSpec work.
- For status, validate, sync, continue, and artifact-editing work, preserve GitHub binding awareness but do not close the bound issue.

## Start

- Read `references/lifecycle.md` before doing lifecycle work.
- Run `openspec list --json` to understand active changes.
- Determine the phase from the request:
  - Proposal: create or continue an OpenSpec change and bind the tracking GitHub issue.
  - Apply: implement `tasks.md` while updating the bound GitHub issue.
  - Merge/Close: merge the PR, let `Resolves #N` auto-close the issue, publish the ADR + blog narrative mirrors.
  - Sync/status/validate/continue/artifact work: keep the GitHub binding in view, preserve the ownership boundary, and avoid premature issue closure.
- If another OpenSpec skill also applies, use this skill as the GitHub lifecycle overlay and follow the base OpenSpec skill for normal artifact generation, task execution, or archive mechanics.

## GitHub Rules

- Prefer the `gh` CLI for GitHub operations: `gh issue create/view/edit/close`, `gh pr create/checks/merge/comment`, `gh run list`, `gh api`.
- This skill aligns agent behavior with the project's enforcement gates — it is advisory, not a gate itself. The AI Reviewer (Rule 70) independently verifies `Resolves` links, issue labels, and OpenSpec-change presence before merge; CI runs `blog-check` (Rule 76) and `adr-sync-check` (Rules 77/78) as hard checks. Follow this skill so agent output passes those gates.
- The tracking GitHub issue is created FIRST (Rule 11, Step 1 of the flying-solo workflow) — before the OpenSpec change exists. Proposal **binds** that issue; there is no backlog-hunting step.
- If `openspec/github.yaml` is missing during proposal/setup, perform setup: read the repo owner/name/branch from `gh repo view` and confirm the issue label filter. Write the config, then bind the existing issue.
- During missing-config setup, call only read-only `gh repo view` / `gh issue view`. Do not create new issues until the configured lifecycle flow requires them.
- Ask setup choices one question at a time: first confirm the repository, then the issue label filter. The label question must include an explicit "no label filter" option.
- Never infer or auto-select assignee, label, or issue from names, ordering, previous results, or seemingly obvious matches.
- After `openspec/github.yaml` exists, GitHub unavailability is non-blocking. Continue local OpenSpec work and skip GitHub updates silently.
- Keep GitHub comments short, at most two sentences.
- Do not make GitHub the specification source of truth. Canonical specs live under `openspec/specs/`.
- Do not close the bound GitHub issue before the PR merges. The `Resolves #N` link in the PR body auto-closes it on merge (Rule 11).
- Keep detailed technical design out of GitHub issue descriptions and comments; keep business context out of repo design/tasks except where needed to make technical decisions traceable.
- When `proposal.md` is created or materially updated, update the bound GitHub issue description with the current business-facing proposal details when possible. Exclude frontmatter, GitHub metadata, detailed technical design, task lists, and implementation notes.

## Phase Hooks

Proposal:
- Load `openspec/github.yaml`; if it is missing, create it first using the missing-config setup rules above.
- Bind the existing tracking issue (created in Step 1 of the workflow) — read its title, description, labels, comments, links, and related context when available.
- Treat the GitHub issue as the business brief. If proposal discovery produces new or corrected business details, update the GitHub issue when possible instead of making `proposal.md` the long-form business record.
- Ask clarifying questions before writing artifacts.
- Keep `proposal.md` lean: record GitHub metadata in frontmatter, include capability impact and a concise repo-facing summary, and link back to the GitHub issue for business details.
- The spec `spec.md` MUST preserve Rule 11 linkage: first line `> GitHub Issue URL: https://github.com/<owner>/<name>/issues/<N>`.
- After writing or updating `proposal.md`, refresh the bound GitHub issue description from the proposal's business-facing sections when possible.
- Ensure the issue carries the `automation` label (and a `bug`/`enhancement` category label) and is assigned, per the Rule 72 ownership gate.

Apply:
- Read `github_issue_id` from `openspec/changes/<change>/proposal.md` frontmatter before implementation.
- Before implementation, sync useful business/context changes from `proposal.md` to the GitHub issue description when possible, but do not copy technical design details from `design.md` into the issue.
- Work through `tasks.md`, marking checkboxes complete as tasks finish.
- Leave the issue open — closure is for merge time via `Resolves #N`.

Merge/Close:
- Complete the normal OpenSpec flow first, including delta spec sync when applicable.
- The feature PR (Step 7) MUST include the OpenSpec change files and its body MUST use `Resolves #N` (Rule 11).
- After the PR merges (`gh pr merge --merge --delete-branch`), the bound issue auto-closes via the `Resolves` link — do not run `gh issue close` as a substitute.
- Publish the narrative mirrors with the change: run `make adr-sync` for the ADR (Rules 77/78) and ship the blog post (Rule 76) in the same PR. These replace the Linear Project Documents mirror.

## Guardrails

- Keep post-merge narrative-mirror guidance out of `tasks.md`; it belongs to merge/close behavior.
- Only ADRs and blog posts are in scope as narrative mirrors. Do not create generic GitHub resources for specs.
- The ADR is an archive-time mirror of the finalized spec, not a place for draft design notes or implementation tasks.
- If the active schema is already `githubized`, honor OpenSpec CLI instructions and avoid duplicating conflicting guidance.
- If the active schema is local-only, layer this skill's GitHub hooks around the normal proposal/apply/merge workflow.