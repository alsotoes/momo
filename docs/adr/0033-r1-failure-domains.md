# 0033-r1-failure-domains

## Status
Accepted

## Confidence
High

## Context
`src/common/crush.go` uses a flat, weight-based ring for replica placement. There is
no concept of a failure domain (rack, data-center, zone). With `replication_factor=3`,
CRUSH may place all three replicas of an object on nodes that share a single point of
failure (same rack/power/network), so a single-domain outage destroys every copy.
Production durability requires copying to be spread across independent failure domains.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/r1-failure-domains/
- Blog: docs/blog/posts/...md
