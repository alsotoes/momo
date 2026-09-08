# GitHubized OpenSpec Lifecycle

This reference distills the GitHub lifecycle into skill behavior. Use it to coordinate proposal, apply, and merge/close lifecycle events while keeping the GitHub issue responsible for the business "what" and the repo responsible for the technical "how".

## Ownership Boundary

- The GitHub issue owns the business "what": business goal, business use cases, user/persona context, workflows, scope, acceptance criteria, and stakeholder-facing status. Keep the issue description synchronized from the business-facing content in `proposal.md`.
- Repo change artifacts own the technical "how": design decisions, architecture trade-offs, data models, migrations, implementation tasks, test plans, and code.
- `proposal.md` is a lightweight repo coordination artifact. It records GitHub binding metadata, capability impact, and a concise repo-facing summary; use its business-facing sections to refresh the GitHub issue description when those details change.
- `design.md` and `tasks.md` stay in the repo. Do not copy detailed technical design, architecture, task lists, or implementation notes into the GitHub issue.
- Canonical specs live under `openspec/specs/`. Narrative mirrors (ADR + blog post) are published **at merge time**, not after archive — see Merge/Close below.
- The ADR and blog post are mirrors, not the canonical source. The ADR is regenerated from spec content by `make adr-sync`; the blog post carries the same PR's journey and evidence.

## Config

Project-local GitHub setup lives at `openspec/github.yaml`.

```yaml
repository:
  owner: alsotoes
  name: momo
  default_branch: master
issue:
  assignee: alsotoes
  label_filter: automation
  category_labels: [bug, enhancement]
close:
  auto_close_via_resolves: true
narrative_mirrors:
  blog: docs/blog/posts
  adr: docs/adr
```

If the file is missing during proposal work:

1. Verify `gh` CLI connectivity (`gh auth status`).
2. Confirm the repository identity with read-only `gh repo view` (owner/name/default branch).
3. Ask one question at a time: first confirm the repository, then the issue label filter.
4. Include an explicit "no label filter" option in the label question.
5. Never infer or auto-select assignee, label, or issue from names, ordering, previous results, or seemingly obvious matches.
6. Write confirmed IDs and names to `openspec/github.yaml`.
7. Return to the configured Proposal Binding flow; bind the existing tracking issue (Rule 11) rather than creating a new one.

If setup cannot verify `gh` connectivity, ask whether to continue locally without an issue binding or pause until GitHub is available. After setup exists, GitHub failures are non-blocking.

## Project Config Policy

This skill layers the GitHub lifecycle over the existing OpenSpec schema. If the project already uses the GitHub binding (repo + issue + PR conventions per Rule 11 / Rule 72), the policy is already encoded in `openspec/config.yaml`. Keep that schema and do not duplicate the policy here.

## Proposal Binding

Proposal frontmatter should include these fields:

```yaml
---
github_issue_id:
github_issue_url:
github_issue_title:
github_issue_state:
github_issue_assignee:
github_issue_labels:
github_pr_id:
---
```

Selection flow:

1. Load `openspec/github.yaml`.
2. Confirm the tracking issue exists (created in Step 1 of the flying-solo workflow). Read its title, description, labels, comments, links, and related context.
3. If the user gives a specific GitHub issue identifier, retrieve/use that issue when it matches the configured context.
4. Treat the issue as the business brief. It may still be incomplete, but new or corrected business facts should be written back to the GitHub issue when possible.
5. Ask clarifying questions covering:
   - business goal and business cases
   - target users, personas, and workflows
   - in-scope and out-of-scope behavior
   - acceptance criteria and affected OpenSpec capabilities
   - architecture constraints and preferred approach, for repo design only
   - tech stack constraints and integration points, for repo design only
   - risks, migrations, rollout constraints, and non-goals
6. Before finishing proposal setup, update the GitHub issue description with confirmed business details from `proposal.md` when possible. Keep technical decisions in repo artifacts.
7. Ensure the issue carries the `automation` label (plus `bug`/`enhancement`) and is assigned per Rule 72; an issue lacking the assignee or `automation` label is orphaned and MUST be remediated before work begins.

Do not infer architecture, tech stack, acceptance criteria, or affected capabilities solely from the GitHub issue unless the issue explicitly states them and the contributor confirms they are current.

## OpenSpec Artifacts

Artifact order remains:

```text
proposal -> specs -> design -> tasks
```

Proposal:
- Keep it lightweight and repo-facing. Record GitHub binding metadata, affected capabilities, and a concise summary needed to coordinate OpenSpec artifacts.
- Use the business-facing sections in `proposal.md` to refresh the bound GitHub issue description when possible.
- If proposal discovery creates or changes business details, update the GitHub issue when possible instead of expanding `proposal.md` into a long-form business brief.
- Description sync must exclude frontmatter, GitHub metadata, detailed technical design, task lists, and implementation notes.
- Include New Capabilities and Modified Capabilities so specs can be generated correctly.
- Research `openspec/specs/` before listing modified capabilities.
- The spec `spec.md` MUST preserve Rule 11 linkage: first line `> GitHub Issue URL: https://github.com/<owner>/<name>/issues/<N>`.

Specs:
- Create one delta spec per capability listed in the proposal.
- Use `## ADDED Requirements`, `## MODIFIED Requirements`, `## REMOVED Requirements`, or `## RENAMED Requirements`.
- Each requirement uses `### Requirement: <name>`.
- Each scenario must use exactly `#### Scenario: <name>`.
- Every requirement must include at least one scenario.
- For modified requirements, copy the entire existing requirement block before editing it.

Design:
- Create only when the change is cross-cutting, introduces a meaningful architecture or data model decision, adds an external dependency, has security/performance/migration complexity, or contains ambiguity that benefits from decisions before coding.
- Include context, goals/non-goals, decisions with alternatives, risks/trade-offs, migration plan when applicable, and open questions.
- Keep design content in the repo. GitHub comments may mention that design exists or was updated, but should not duplicate the detailed design.

Tasks:
- Use checkboxes exactly: `- [ ] X.Y Task description`.
- Keep tasks small, dependency ordered, and directly verifiable.
- Include relevant tests and `openspec validate <change> --strict`.
- If the change modifies `openspec/schemas/`, include `openspec schema validate`.
- Do not add post-merge narrative-mirror checkboxes (ADR/blog) — those belong to merge/close behavior.

## Apply Hook

Before implementation:

1. Read `openspec/changes/<change>/proposal.md` frontmatter.
2. If `github_issue_id` is present and `gh` is available, sync confirmed business/context changes from `proposal.md` to the issue description when possible.
3. Add a GitHub comment of at most two sentences saying implementation began from the OpenSpec change and tracked tasks are being applied.

Then follow the normal OpenSpec apply workflow:

- Read context files from `openspec instructions apply --change "<change>" --json`.
- Work through pending tasks in `tasks.md`.
- Mark each checkbox complete immediately after the task is done.
- Pause for blockers, unclear requirements, or design issues.

Never close the issue during apply. Closure happens at merge time via `Resolves #N`.
Do not copy `design.md` or `tasks.md` into the GitHub issue during apply; the issue should show status and business context, while repo artifacts carry implementation detail.

## Merge/Close Hook

Order is strict:

1. Complete normal OpenSpec flow, including delta spec sync when the user chooses to sync.
2. Ensure the feature PR (Step 7) includes the OpenSpec change files and its body uses `Resolves #N` (Rule 11).
3. After all checks pass and the reviewer approves, merge with `gh pr merge --merge --delete-branch`.
4. The bound issue auto-closes via the `Resolves` link — do not run `gh issue close` as a substitute.
5. Publish the narrative mirrors with the change:
   - Run `make adr-sync` to generate/refresh the ADR in `docs/adr/NNNN-<change-id>.md` (Rules 77/78). Never hand-edit an ADR.
   - Ship the blog post under `docs/blog/posts/NNN-<slug>.md` (Rule 76), with `date` = the anchor issue/PR `createdAt`, unless a `no-blog` justification applies.

The ADR and blog post are merge-time mirrors of the finalized change. They are generated from repo artifacts, not a separate backlog of work.

## GitHub Tool Mapping

Use the `gh` CLI for all GitHub operations:

- Repository identity: `gh repo view` / `gh repo view --json defaultBranchRef`
- Issue binding: `gh issue view <N> --json title,body,labels,assignees,state`
- Issue updates: `gh issue edit <N> --add-label <label> --add-assignee <user>`
- Issue comments: `gh issue comment <N> --body "<text>"`
- PR creation: `gh pr create --base master --title "<title>" --body "Resolves #<N> ..."`
- PR checks: `gh pr checks <N>`
- PR merge: `gh pr merge <N> --merge --delete-branch`
- CI runs: `gh run list --branch master`

Use names or IDs accepted by the tools. Prefer issue numbers when already stored in `openspec/github.yaml` or the proposal frontmatter.