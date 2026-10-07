# Spec: Reviewer REST mutations + robust approval detection

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1140

## Requirements

### RRM-A1: PR/issue mutations use the REST API

**Requirement:** The reviewer MUST perform label, assignee, PR-body, and issue
creation mutations through the REST API (`gh api`), not `gh pr edit` /
`gh issue edit` / `gh issue create`, so they succeed with a `repo`-scoped token.

**Scenario: Labels and assignee are applied with a repo-only token**

Given the reviewer runs with a token that has only the `repo` scope,
When it syncs labels and assigns the maintainer to a PR,
Then it issues `POST /repos/{owner}/{repo}/issues/{n}/labels` and
`POST /repos/{owner}/{repo}/issues/{n}/assignees`,
And the labels/assignee are applied (no GraphQL `read:org` error).

**Scenario: The auto-trace issue is created and linked via REST**

Given a PR with no tracking issue,
When the reviewer creates the auto-trace issue,
Then it uses `POST /repos/{owner}/{repo}/issues` and sets the PR body with
`PATCH /repos/{owner}/{repo}/pulls/{n}`,
And the PR body carries `Resolves #<n>`.

### RRM-A2: Approval detection is robust to formatting

**Requirement:** `review_is_approved()` MUST detect an approval even when the
review is prefixed with `@google-labs-jules` and/or structured headings.

**Scenario: A Jules-prefixed approval is detected**

Given the review text begins with `@google-labs-jules` followed by the
canonical approval sentence,
When `review_is_approved()` is called,
Then it returns true.

**Scenario: An appended ECC block does not cause approval**

Given a findings review with an appended `**ECC audit**` block containing `✅`,
When `review_is_approved()` is called,
Then it returns false.

### RRM-A3: Blockers are never treated as approval

**Requirement:** A review containing an explicit blocker MUST NOT be approved.

**Scenario: Violation markers withhold approval**

Given a review containing `🚨`, `🛑`, or `VIOLATION`,
When `review_is_approved()` is called,
Then it returns false, even if a `✅` is also present.

### RRM-A4: Auto-trace issues are categorized

**Requirement:** Auto-trace issues MUST be labeled by PR category.

**Scenario: Sentinel PRs get the bug label**

Given a Sentinel (security/bug) PR title,
When the reviewer creates the auto-trace issue,
Then the issue is labeled `bug` and `automation`.

**Scenario: Feature PRs get the enhancement label**

Given a non-Sentinel PR title,
When the reviewer creates the auto-trace issue,
Then the issue is labeled `enhancement` and `automation`.

## Non-Goals

- No change to the review prompt, the model, or the merge command.
- No change to the token's granted scopes (REST makes this unnecessary).
