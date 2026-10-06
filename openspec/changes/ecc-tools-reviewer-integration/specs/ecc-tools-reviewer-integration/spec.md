> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1134

# ecc-tools-reviewer-integration Specification

## Purpose

Integrate the ECC Tools GitHub App's advisory PR audits into momo's AI reviewer, and
auto-triage the app's unsolicited "ECC bundle" PRs, without changing merge gating.

## ADDED Requirements

### Requirement: reviewer surfaces ECC audit verdicts

The AI reviewer SHALL read the PR's `ecc-tools` audit comments and append a compact
summary of the latest verdict per dimension to the posted review, when such comments exist.

#### Scenario: ECC audits present
- **GIVEN** the PR has one or more comments authored by `ecc-tools` carrying the marker
  `<!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->`
- **WHEN** the reviewer posts its review
- **THEN** the review includes an `ECC audit — …` block listing each dimension and its
  latest verdict (mapped to ✅ success / ⚠️ neutral / ❌ failure)
- **AND** the block is appended after the model-generated review text

#### Scenario: ECC audits absent
- **GIVEN** the PR has no `ecc-tools` audit comments
- **WHEN** the reviewer posts its review
- **THEN** the review is unchanged and no ECC block is added
- **AND** no error is raised

### Requirement: ECC verdicts never affect merge gating

The ECC audit summary SHALL be advisory and SHALL NOT change the approve or auto-merge
decision.

#### Scenario: ECC flags a dimension but momo's reviewer approves
- **GIVEN** the model-generated review starts with `✅` (approved)
- **AND** an ECC dimension reports a non-success status
- **WHEN** the reviewer completes
- **THEN** `is_approved` remains true and the auto-merge gate proceeds unchanged

### Requirement: unsolicited ECC bundle PRs are auto-closed

The repository SHALL close pull requests authored by `ecc-tools[bot]` whose title matches
"ECC bundle", posting a rationale comment first.

#### Scenario: ECC bundle PR opened
- **GIVEN** a pull request opened/reopened/synchronized by `ecc-tools[bot]`
- **WHEN** its title contains "ECC bundle"
- **THEN** the workflow posts a comment explaining that momo does not adopt auto-generated
  bundles and closes the PR

#### Scenario: non-bundle PR from the app
- **GIVEN** a pull request authored by `ecc-tools[bot]` whose title does NOT contain
  "ECC bundle"
- **WHEN** the workflow runs
- **THEN** the PR is left untouched
