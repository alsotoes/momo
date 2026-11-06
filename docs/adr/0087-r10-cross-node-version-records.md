# 0087-r10-cross-node-version-records

## Status
Accepted

## Confidence
High

## Context
Bucket versioning records live in the writing node's BoltDB. In a multi-node
deployment an S3 write handled by node A registers a version only on A, so
`GET ?versions`, version-addressed GET/HEAD/DELETE, and object-lock enforcement
consulted from node B do not see that version. Versioning therefore behaves
correctly only on the node that served the write.

The original sketch proposed reusing the R6 metadata RPC ring. Inspection
showed the R6 client path is scaffolding: `PutMetadata`/`ResolveMetadata`/
`ReplicateMetadata`/`ListShard` return canned replies and no server wires the
provider. Rather than build a second control plane, this change reuses the P2P
transport that already carries gossip and version metadata is co-located with
the blob copies themselves.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/061-cross-node-version-records.md

## References
- Issue: #1233
- PR: 
- Spec: `openspec/changes/r10-cross-node-version-records/`
- Blog: docs/blog/posts/061-cross-node-version-records.md

