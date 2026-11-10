# 0091-denial-observability

## Status
Accepted

## Confidence
High

## Context
The audit log records *completed* operations, so the requests an operator
investigates — the refused ones — leave no trace. An ACL denial returns `403` and
closes the connection; an over-quota write stores nothing. Neither writes an audit
entry, and no metric moves. "Nobody is using this bucket" and "every tenant is
being denied" look identical on a dashboard.

## Decision
- Refusals are audited with a denied outcome: An authorization or quota refusal SHALL append an audit entry whose `outcome` is `denied`, attributed to the authenticated tenant.
- Refusals are counted: The daemon SHALL export a counter for authorization denials and a counter for quota refusals, incremented once per refused request.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/071-making-refusals-visible.md

## References
- Issue: #1319
- PR: 
- Spec: `openspec/changes/denial-observability/`
- Blog: docs/blog/posts/071-making-refusals-visible.md

