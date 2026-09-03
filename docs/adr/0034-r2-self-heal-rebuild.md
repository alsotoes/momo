# 0034-r2-self-heal-rebuild

## Status
Accepted

## Confidence
High

## Context
`storage-at-rest-integrity` (#924) added verify-on-read and a background scrub that
**quarantines** corrupt blobs. But quarantine is delete-only: a corrupt copy is removed
and later reads get `ENOENT`. There is no cross-replica healing — if one replica of an
object is corrupt/underrreplicated while others hold good bytes, momo does not regenerate
it from the survivors. A single lost node reduces `replication_factor` to `factor-1`
permanently (no background repair). Production durability requires degraded reads and a
self-healing re-replication loop.

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
- Spec: openspec/changes/r2-self-heal-rebuild/
- Blog: docs/blog/posts/...md
