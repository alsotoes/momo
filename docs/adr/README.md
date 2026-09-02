# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records for the Momo project, following [Martin Fowler's ADR pattern](https://martinfowler.com/bliki/ArchitectureDecisionRecord.html) and [Michael Nygard's original formulation](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

## Overview

Each ADR corresponds to an **OpenSpec change** under `openspec/changes/<change-id>/`. The OpenSpec change is the authoritative source containing:
- `proposal.md` — context, problem statement, alternatives
- `specs/<change-id>/spec.md` — detailed requirements
- `tasks.md` — implementation tracking

The ADR serves as a **narrative index** linking the spec to its GitHub issue, PR, and blog post.

## ADR Format

```markdown
# NNNN-Spec-Name

## Status
Accepted | Proposed | Deprecated

## Spec Reference
`openspec/changes/<change-id>/`

## Context
[From spec's proposal.md — why this change?]

## Decision
[From spec's spec.md — what is being done?]

## Consequences
[From spec — trade-offs, what's easier/harder]

## Alternatives Considered
[From spec — options evaluated]

## Implementation Status
- **Code**: [Done/Partial/Planned]
- **Tests**: [Done/Partial/Planned]
- **Docs**: [Done/Partial/Planned]
- **Blog post**: [Linked or no-blog justification]

## References
- Issue: #NNN
- PR: #NNN
- Spec: `openspec/changes/<change-id>/`
- Blog: `docs/blog/posts/NNN-...md`
```

## Status Definitions

| Status | Meaning |
|--------|---------|
| **Accepted** | Spec ratified, code merged, tests passing, docs updated |
| **Proposed** | Spec ratified but implementation incomplete; or spec not yet ratified |
| **Deprecated** | Spec abandoned or superseded |

## Process

1. **New architectural decision** → Create OpenSpec change under `openspec/changes/<id>/` (Rule 73)
2. **During implementation** → Create ADR in this directory when PR is opened
3. **On merge** → Update ADR status to `Accepted`, link PR/issue/blog
4. **If decision changes** → New ADR with `Supersedes` reference; old ADR marked `Deprecated`

## Naming Convention

`NNNN-<change-id>.md` where `NNNN` is a 4-digit sequential number and `<change-id>` matches the OpenSpec directory name.

## Cross-References

- OpenSpec changes: `openspec/changes/`
- Blog posts: `docs/blog/posts/`
- Issues: GitHub Issues (linked via `Resolves #NNN` in PR body)

## Maintenance

- ADRs are **never modified after acceptance** (except status updates)
- Superseded decisions get a new ADR with `Supersedes` link
- The `template.md` file contains the standard template
