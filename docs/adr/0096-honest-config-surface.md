# 0096-honest-config-surface

## Status
Accepted

## Confidence
High

## Context
Auditing every `Configuration*` field for read sites outside the loader found four
keys the daemon accepts and never honours. A key that is parsed and validated but
never read is worse than a missing key: it advertises a guarantee the system does
not have. The worst of the four is `write_quorum`, whose documentation promises a
write fails when fewer than N durable replicas acknowledge — while replica
acknowledgements are not collected at all.

## Decision
- Configured metadata quorum is honoured: The metadata write and delete quorum SHALL be the configured `[momofs] metadata_quorum` when set, clamped to the replica-set size, and a strict majority of the replica set when unset.
- Unimplemented keys are refused at load: A key the daemon does not honour SHALL cause a load error that names the key, says it is not implemented and links the tracking issue. Defaults SHALL keep loading.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/072-configuration-that-cannot-lie.md

## References
- Issue: #1323
- PR: 
- Spec: `openspec/changes/honest-config-surface/`
- Blog: docs/blog/posts/072-configuration-that-cannot-lie.md

