> GitHub Issue URL: https://github.com/alsotoes/momo/issues/908

# steering-rule-73-spec-first Specification

## Purpose
Codify the Spec-First Implementation Mandate so every new feature or
architectural change documents its design in an OpenSpec change linked to a
GitHub issue **before** implementation, with the spec shipped in the same PR.
This is a process/governance rule; it does not alter runtime behavior.

## ADDED Requirements

### Requirement: spec-first for features
The system (all AI agents) SHALL author `openspec/changes/<id>/` with
`proposal.md`, `specs/<id>/spec.md`, and `tasks.md` for any new feature,
spec-driven change, or architectural shift, before writing implementation code.
The `spec.md` SHALL link the GitHub issue at the very top.

#### Scenario: new feature has a spec + issue
- **GIVEN** a new feature to be implemented and a tracking GitHub issue
- **WHEN** the feature branch is created
- **THEN** an OpenSpec change (`proposal.md`, `specs/<id>/spec.md`, `tasks.md`) is authored on the branch before code, and `spec.md` links the issue

#### Scenario: feature PR carries the spec + Resolves
- **GIVEN** a feature Pull Request
- **WHEN** it is opened
- **THEN** it includes the OpenSpec change and its body uses `Resolves #ISSUE_ID`

### Requirement: reviewer enforcement
The AI reviewer SHALL flag an enhancement PR that ships without an OpenSpec
change under `openspec/changes/` and MUST NOT approve/merge until one is added.

#### Scenario: enhancement PR without spec flagged
- **GIVEN** an enhancement-labeled PR that changes no files under `openspec/changes/`
- **WHEN** the reviewer inspects it
- **THEN** it reports a Rule 73 violation and withholds approval/merge

### Requirement: bug-fix exemption
Trivial internal bug fixes or refactors with no behavioral surface are exempt
from a formal spec but SHALL still have a tracking GitHub issue.

#### Scenario: bug fix, no formal spec
- **GIVEN** a trivial internal bug fix with a tracking issue
- **WHEN** it is implemented
- **THEN** no formal OpenSpec change is required; the tracking issue satisfies the mandate

## UNCHANGED Behavior
- No change to runtime, storage, protocol, or transport behavior.
- Existing rectified in-flight PRs and already-ratified specs are unaffected.
- Rule 11 (issue-spec traceability) and Rule 39 (single source of truth) are
  preserved; the steering rules remain defined only in `openspec/config.yaml`.
