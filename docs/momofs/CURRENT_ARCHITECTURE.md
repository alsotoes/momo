# Current Database Architecture

## 1. Current Database Architecture

### 1.1 Two Local BoltDB Databases Per Node

BoltDB is an embedded key-value store. Each node in the cluster has its own isolated databases. **Metadata is never shared across nodes.**

| Database | File | Owner | Scope |
|----------|------|-------|-------|
| Metadata DB | `<dataDir>/momo.db` | `CASStore` | Local node only |
| Raw Allocation DB | `<dataDir>/raw_alloc.db` | `RawBlobStore` | Local node only (raw backend only) |

### 1.2 Metadata DB Schema (`momo.db`)

Four buckets, all local to the node:

#### Bucket: `objects`
- **Key**: content hash (SHA-256 hex string)
- **Value**: 24-byte big-endian binary `[Size(8B)][RefCount(8B)][DeletedAt(8B)]`
- **Purpose**: Per-blob metadata. Tracks size, deduplication reference count, and soft-delete timestamp.
- **Lifecycle**: Created on `Put` (RefCount=1). RefCount incremented on dedup hit, decremented on `Delete`. GC removes entries with RefCount=0.

#### Bucket: `namespace`
- **Key**: file name (UTF-8 string)
- **Value**: content hash (UTF-8 string)
- **Purpose**: Maps user-facing file names to content-addressed hashes. The primary lookup index for `Get(name)`.
- **Lifecycle**: Created on `Put`. Deleted on `Delete` or `ApplyTombstone`. Multiple names can point to the same hash (deduplication).

#### Bucket: `paths`
- **Key**: file name (UTF-8 string)
- **Value**: normalized virtual remote path (UTF-8 string, max 64 bytes)
- **Purpose**: Stores the virtual directory/folder path associated with a file. Only written when `remotePath != ""`.
- **Lifecycle**: Created on `Put` with remotePath. Deleted on `Delete` or `ApplyTombstone`.

#### Bucket: `tombstones`
- **Key**: file name (UTF-8 string)
- **Value**: 8-byte big-endian uint64 unix-nano deletion timestamp
- **Purpose**: Soft-delete marker. Hides deleted files from `Get`/`List`/`GetBlobPath`. Enables resurrection (re-Put clears tombstone). Propagated via P2P gossip for eventual consistency.
- **Lifecycle**: Created on `Delete`. Cleared on resurrection (`Put`). Expired by GC after `TombstoneRetention` (default 24h).

### 1.3 Raw Allocation DB Schema (`raw_alloc.db`)

#### Bucket: `raw_alloc`
- **Key**: content hash (UTF-8 string)
- **Value**: 16-byte big-endian binary `[offset(8B)][length(8B)]`
- **Purpose**: Bump-allocator table for raw block device backend. Maps each blob to its physical offset and length on the device.
- **Lifecycle**: Created on `PutBlob`. Deleted on `DeleteBlob` (space not reclaimed — no compaction). Scanned on restart to recover `nextOffset`.

### 1.4 What IS Shared Across Nodes

| Mechanism | What it syncs | How |
|-----------|--------------|-----|
| CRUSH placement | Data location (which nodes hold which blobs) | Deterministic computation from cluster map + content hash — no shared state needed |
| Replication (Chain/Splay) | Blob bytes | Active push from primary to secondaries during write |
| P2P gossip | Tombstones (deletes) only | `GetTombstones` / `ApplyTombstone` via scatter-gather |
| P2P gossip | Cluster membership (alive/suspect/dead) | SWIM-style heartbeats |
| Lease consensus | Coordinated operations | Quorum-based leases for brief operations |

### 1.5 What is NOT Shared (The Gap)

| Missing | Impact |
|---------|--------|
| **Global namespace** | If client writes `foo.txt` to node A, node B doesn't know it exists unless B is a replica target |
| **Global directory tree** | No cluster-wide `List("/")` — each node only lists its own local files |
| **Metadata replication** | Namespace mappings, refcounts, paths are local-only — no metadata replication strategy |
| **Cluster-wide object index** | No way to find which node holds a given object without asking all nodes (scatter-gather) |
| **Consistent cluster state** | No distributed transaction or consensus on metadata writes |
| **Placement metadata** | CRUSH computes placement, but there's no record of *actual* placement (which nodes actually have the data) |

