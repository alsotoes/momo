# 0005-add-cas-storage

## Status
Accepted

## Confidence
High

## Context
The current storage model is name-based and relies on a fixed primary node (Node 0). To transform Momo into a truly scalable, high-performance **Object Storage system**, we must:
1.  **Deduplicate Data**: Identify files by their content hash (SHA-256) to save space and replication bandwidth.
2.  **Eliminate Central Bottlenecks**: Move from a "Node 0 is Primary" model to a "Deterministic Placement" model.
3.  **Harden Metadata**: Use a robust, ACID-compliant database for metadata rather than simple directory structures.

## Decision
- Content-Based File Addressing: The system SHALL store and retrieve files based on a cryptographic hash of their content.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-cas-storage/
- Blog: docs/blog/posts/...md
