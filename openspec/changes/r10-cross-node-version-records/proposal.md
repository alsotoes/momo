# Change: R10 follow-up — Replicate Version Records Across Nodes

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1233 (R10 follow-up: replicate version records across nodes)
- https://github.com/alsotoes/momo/issues/938 (Parent: R10 S3 breadth)

## Why

Bucket versioning records live in the writing node's BoltDB. In a multi-node
deployment an S3 write handled by node A registers a version only on A, so
`GET ?versions`, version-addressed GET/HEAD/DELETE, and object-lock enforcement
consulted from node B do not see that version. Versioning therefore behaves
correctly only on the node that served the write.

The original sketch proposed reusing the R6 metadata RPC ring. Inspection showed
that ring is the wrong carrier for this job: the R6 provider is wired for
quorum metadata *writes* and *reads* of blob metadata, but it is keyed by shard
and replica set, whereas a version record must reach the object's **data**
replicas, and listing was never part of it at all. Rather than build a second
control plane, this change reuses the P2P transport that already carries gossip,
so a version record is co-located with the blob copies themselves.

## What Changes

- Add a storage primitive `PutVersionRecord` that applies a caller-supplied
  record verbatim: idempotent by version ID, never demoting a newer local
  record to "latest".
- Add a version-replication hook on the CAS store, fired only for locally
  created records (never for replicated ones, so replication cannot loop).
- Add P2P messages `MsgReplicateVersion` / `MsgReplicateVersionResponse`
  carrying the record plus the bucket's versioning status, with an
  acknowledged request/response call bounded by a timeout.
- Wire the fan-out in the P2P bootstrap: after a local version record is
  written, send it to the object's CRUSH replica set (excluding self), using
  the blob hash as the placement key and the object name for delete markers.
- Best-effort semantics: failures are audit-logged, never fatal. The local
  record is already durable and the next write re-replicates the chain.

## Impact

- Affected specs: `r10-version-replication`
- Affected code: `src/storage/versioning.go`, `src/storage/storage.go`,
  `src/p2p/metadata_rpc.go`, `src/p2p/types.go`, `src/server/server.go`,
  `src/server/version_replication.go`
- Ceiling retained: plain `PUT` still cannot echo `x-amz-version-id`
  (the response precedes body streaming; multipart does).
