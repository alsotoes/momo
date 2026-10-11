> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1184

# Spec: Post-Merge Master Gate Verification (Rule 94)

## Requirements

### R94-V1: Merge is not done

**Requirement:** Work MUST NOT be reported as complete until the full master pipeline has settled on the merge commit and every gate — including the SonarCloud master quality gate — reports success.

**Scenario: green PR, green master**

Given a PR merged into master,
When the full run set on the merge SHA finishes and the Sonar master gate is OK,
Then the work may be reported as done.

**Scenario: PR green, master red**

Given a PR merged with all PR checks green,
When master's post-merge analysis fails any gate,
Then the same session opens a hotfix immediately (fix → PR → re-verify → confirm master OK), and the user is informed of the failure and the fix — not left to discover it.

### R94-V2: PR evidence is not master evidence

**Requirement:** PR-level check results MUST NOT be used as evidence that master is green.

**Scenario: premature declaration**

Given all PR checks pass,
When the agent has not yet observed the master gate on the merge SHA,
Then any "done / master green / Sonar OK" statement is a process violation.

### R94-V3: Necessary-but-insufficient local signals

**Requirement:** Local verification (`make lint`, `make test`) MUST be treated as necessary but not sufficient, because Sonar's leak-period analysis re-dates moved or reformatted code that local new-mode lint cannot see.

**Scenario: line-shift exposure**

Given a PR whose edits shift existing code within a file,
When Sonar's master leak-period analysis re-dates the shifted region and reports new issues,
Then the rule's hotfix duty applies without argument about whose code the issue "really" belongs to.
