# 0094-reviewer-deterministic-gates

## Status
Proposed

## Confidence
Low

## Context
The AI reviewer computes artifact presence from the repository, but the model
writing the review prose has repeatedly contradicted those computations: on PR
#1304 it claimed the ADR, the `no-blog` justification, and the `Resolves #1303`
link were all missing while every one of them was in the diff and the PR body. Each
false claim blocked the merge and cost a re-review cycle.

## Decision
- The no-blog exemption is computed from artifacts: The reviewer SHALL treat Rule 76 as exempt when the PR body mentions `no-blog`, or when the branch adds a `docs/adr/*.no-blog.md` file. A Git failure SHALL NOT count as an exemption.
- Artifact facts are authoritative in the prompt: The reviewer SHALL render the computed artifact presence (OpenSpec change, blog post, no-blog exemption, issue link, enhancement label) into the prompt and instruct the model not to claim a present artifact is missing.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1305
- PR: 
- Spec: `openspec/changes/reviewer-deterministic-gates/`
- Blog: 

