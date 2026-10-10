# Spec: Pre-merge complexity gates

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1172

## Requirements

### LCG-A1: Local complexity gate mirrors the Sonar master gate

**Requirement:** The repository MUST provide `make lint`, running golangci-lint
with `gocognit` and `gocyclo` at a minimum complexity of 15 — the same metric
and threshold SonarCloud enforces via `go:S3776` — so a green local run predicts
a green Sonar analysis.

**Scenario: A function above the budget is flagged before merge**

Given a PR that adds a function with cognitive complexity > 15,
When `make lint` (or the CI lint job) runs,
Then the function is reported with a `gocognit` finding,
And the PR cannot be considered clean until the function is decomposed or an
exception is documented and reviewer-approved.

### LCG-A2: Reporting is new-code-only with legacy grandfathering

**Requirement:** `.golangci.yml` MUST set `issues.new: true`, so only lines a PR
adds or changes are reported. Legacy functions above the budget (e.g.
`HandshakeServer`, `Daemon`) MUST remain unflagged until a PR edits them, and
thresholds MUST NOT be raised to silence findings.

**Scenario: Master is green under the new gate**

Given the current master (with legacy functions above 15),
When `make lint` runs,
Then no findings are reported.

### LCG-A3: CI enforces the gate on every PR

**Requirement:** A `Lint` workflow MUST run `golangci-lint` on every PR to
master with `only-new-issues`, so the gate is enforced without relying on local
discipline.

**Scenario: The gate runs in CI**

Given a PR to master,
When the Lint workflow runs,
Then it reports only new-code findings and fails the PR when the budget is
exceeded.

### LCG-A4: The standard is documented

**Requirement:** `docs/CORE/STANDARDS.md` MUST document the complexity budget:
the gates table, the ≤ 15 rule for new functions, the prohibition on raising
thresholds, legacy grandfathering semantics, and the requirement that every
error branch of new code is tested (new-code coverage ≥ 80%).
