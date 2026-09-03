# 0036-r4-momofs

## Status
Proposed

## Confidence
High

## Context
Momo is a replicated content-addressed object store + S3 gateway, but is **not** a POSIX
filesystem. The extensive `docs/momofs/` design set (IMPLEMENTATION.md, ARCHITECTURE.md,
DESIGN_DECISIONS.md, PERFORMANCE_SECURITY.md, RECOVERY.md, SCRUB_HEALING.md, MULTI_TENANCY.md,
GDPR.md, COMPARISON.md, LIMITATIONS.md) is design-only — there is no `momofs` source, no
mount command, and no FUSE integration. To be a production **distributed filesystem**, momo
needs a mountable POSIX surface with correct metadata semantics over the replicated CAS.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/r4-momofs/
- Blog: docs/blog/posts/...md
