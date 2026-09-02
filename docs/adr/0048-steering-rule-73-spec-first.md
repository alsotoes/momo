# 0048-steering-rule-73-spec-first

## Status
Proposed

## Confidence
Medium

## Context
Feature/spec-driven work occasionally shipped with only a tracking issue and no
OpenSpec change, or authored the spec after the code. To make spec-first
governance a hard gate, all new implementations MUST document their design in an
OpenSpec change linked to a GitHub issue before code lands.

## Decision
- spec-first for features: The system (all AI agents) SHALL author `openspec/changes/<id>/` with `proposal.md`, `specs/<id>/spec.md`, and `tasks.md` for any new feature, spec-driven change, or architectural shift, before writing implementation code. The `spec.md` SHALL link the GitHub issue at the very top.
- reviewer enforcement: The AI reviewer SHALL flag an enhancement PR that ships without an OpenSpec change under `openspec/changes/` and MUST NOT approve/merge until one is added.
- bug-fix exemption: Trivial internal bug fixes or refactors with no behavioral surface are exempt from a formal spec but SHALL still have a tracking GitHub issue. ## UNCHANGED Behavior - No change to runtime, storage, protocol, or transport behavior. - Existing rectified in-flight PRs and already-ratified specs are unaffected. - Rule 11 (issue-spec traceability) and Rule 39 (single source of truth) are preserved; the steering rules remain defined only in `openspec/config.yaml`.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/steering-rule-73-spec-first/
- Blog: docs/blog/posts/...md
