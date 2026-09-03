# 0032-prod-ready-roadmap

## Status
Proposed

## Confidence
Low

## Context
Momo today is a replicated, content-addressed **blob store** with an S3-compatible
gateway and P2P gossip membership. It is not yet a **production-ready distributed
**filesystem**: the FUSE/POSIX layer (`docs/momofs/`) is design-only, metadata is
per-node bbolt (no distributed catalog/consensus), and several correctness and
operability guarantees are missing. A documentation audit (`docs/*.md`, PR #927)
confirmed the gap surface. This change ratifies the **prioritized production-readiness
roadmap** and gates future work.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/prod-ready-roadmap/
- Blog: docs/blog/posts/...md
