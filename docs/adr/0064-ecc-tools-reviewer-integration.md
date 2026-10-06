# 0064-ecc-tools-reviewer-integration

## Status
Accepted

## Confidence
High

## Context
The ECC Tools GitHub App audits every PR and posts one comment per dimension — Security
Evidence, PR Risk Taxonomy, Reference Set Readiness, Hosted Promotion Readiness — each
carrying a marker of the form
`<!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->`. Those results are currently
disconnected from momo's own reviewer, so a human has to read two places to see the full
picture. Separately, the app can open unsolicited "ECC bundle" PRs (see #1126) that always
target harnesses momo does not use (Claude Code / Codex); triaging them by hand is noise.

This change wires the app into momo's automation in two narrow, advisory directions.

## Decision
- reviewer surfaces ECC audit verdicts: The AI reviewer SHALL read the PR's `ecc-tools` audit comments and append a compact summary of the latest verdict per dimension to the posted review, when such comments exist.
- ECC verdicts never affect merge gating: The ECC audit summary SHALL be advisory and SHALL NOT change the approve or auto-merge decision.
- unsolicited ECC bundle PRs are auto-closed: The repository SHALL close pull requests authored by `ecc-tools[bot]` whose title matches "ECC bundle", posting a rationale comment first.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1134
- PR: 
- Spec: `openspec/changes/ecc-tools-reviewer-integration/`
- Blog: 

