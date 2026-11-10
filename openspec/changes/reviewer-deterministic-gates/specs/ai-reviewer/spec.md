# AI reviewer — deterministic artifact gates

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1305

## Purpose

Keep the reviewer's artifact-presence decisions derived from the repository, and
prevent its prose from contradicting those decisions.

### Requirement: The no-blog exemption is computed from artifacts

The reviewer SHALL treat Rule 76 as exempt when the PR body mentions `no-blog`, or
when the branch adds a `docs/adr/*.no-blog.md` file. A Git failure SHALL NOT count
as an exemption.

#### Scenario: Justification file on the branch
- **Given** a PR whose branch adds `docs/adr/0092-x.no-blog.md`
- **When** the reviewer evaluates Rule 76
- **Then** no Rule 76 violation is injected, even if the body is silent

#### Scenario: Body-only justification
- **Given** a PR whose body contains `no-blog: internal tooling`
- **When** the reviewer evaluates Rule 76
- **Then** no Rule 76 violation is injected

### Requirement: Artifact facts are authoritative in the prompt

The reviewer SHALL render the computed artifact presence (OpenSpec change, blog
post, no-blog exemption, issue link, enhancement label) into the prompt and instruct
the model not to claim a present artifact is missing.

#### Scenario: All artifacts present
- **Given** the gates report every artifact present
- **When** the review prompt is assembled
- **Then** it contains the facts block and no violation instructions
