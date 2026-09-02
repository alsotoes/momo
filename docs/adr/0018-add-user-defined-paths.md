# 0018-add-user-defined-paths

## Status
Proposed

## Confidence
Low

## Context
Users need a way to organize and reference uploaded files using human-readable, hierarchical directory paths (e.g., `customer01/documents/invoice.pdf`). 
Storing these paths directly on the storage node filesystems (hierarchical path storage) breaks Content-Addressable Storage (CAS) deduplication, degrades load-balancing efficiency via the CRUSH-lite algorithm, and violates Rule 12 (Object Storage Paradigm).
By storing user-defined paths purely as virtual metadata mapping to the content hash, we can deliver complete logical organization without breaking Momo's core algorithmic scalability or deduplication capabilities.

## Decision
- User-Defined Path Storage & Retrieve: Momo MUST support storing and retrieving a user-defined virtual folder/directory path alongside standard file metadata, without altering the physical CAS store layout.
- Virtual Path Normalization & Sanitization: Momo MUST normalize and sanitize all virtual paths before storing them in the metadata index to prevent duplicate records, whitespace inconsistencies, or path-delimiter variations.
- Conflict Resolution & Overwrite: The Bbolt metadata index MUST safely handle situations where a new upload request targets an existing, already-indexed virtual path.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-user-defined-paths/
- Blog: docs/blog/posts/...md
