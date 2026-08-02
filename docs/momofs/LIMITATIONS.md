# Current Limitations & Architecture Gap

## What is NOT Shared (The Gap)

### 1.5 What is NOT Shared (The Gap)

| Missing | Impact |
|---------|--------|
| **Global namespace** | If client writes `foo.txt` to node A, node B doesn't know it exists unless B is a replica target. Note: namespace collision (same base name in different directories) is fixed — namespace is keyed by full virtual path (#548). |
| **Global directory tree** | No cluster-wide `List("/")` — each node only lists its own local files |
| **Metadata replication** | Namespace mappings, refcounts, paths are local-only — no metadata replication strategy |
| **Cluster-wide object index** | No way to find which node holds a given object without asking all nodes (scatter-gather) |
| **Consistent cluster state** | No distributed transaction or consensus on metadata writes |
| **Placement metadata** | CRUSH computes placement, but there's no record of *actual* placement (which nodes actually have the data) |


---

## 3. Architecture: From Local Metadata to Distributed Metadata

### 3.1 Current: Local-Only Metadata

```
Node A                          Node B
┌──────────────────┐           ┌──────────────────┐
│ momo.db (local)  │           │ momo.db (local)  │
│  namespace: A's  │           │  namespace: B's  │
│  files only      │          9         │  files only      │
│  objects: A's    │  gossip   │  objects: B's    │
│  refcounts only  │  tombstones│  refcounts only  │
│  tombstones      │◄─────────►│  tombstones      │
└──────────────────┘           └──────────────────┘
        │                              │
        ▼                              ▼
   Local/S3 blobs                 Local/S3 blobs
```

**Problem**: Node A cannot serve a request for a file that was written to Node B unless A is a replica target.

### 3.2 Target: Distributed Metadata with Consistent Hashing

```
         ┌─────────────────────────────────────────────────┐
         │            Distributed Metadata Layer             │
         │  (consistent hashing → metadata partition owner)  │
         └─────────────────────────────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         ▼                    ▼                    ▼
    Node A (shard 0-3)    Node B (shard 4-7)    Node C (shard 8-B)
    ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
    │ momo.db      │     │ momo.db      │     │ momo.db      │
    │ shard 0-3    │     │ shard 4-7    │     │ shard 8-B    │
    │ + replicas   │     │ + replicas   │     │ + replicas   │
    │ of 4-7, 8-B  │     │ of 0-3, 8-B  │     │ of 0-3, 4-7  │
    └──────────────┘     └──────────────┘     └──────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
    Blob backend         Blob backend         Blob backend
    (local/S3/NFS)       (local/S3/NFS)       (local/S3/NFS)
```

**Key change**: Metadata is **partitioned** across nodes using consistent hashing. Each metadata shard is replicated to N nodes (metadata replication factor, separate from data replication factor). Any node can find any metadata by computing the shard owner.

### 3.3 Metadata Replication Protocol

```
Client writes foo.txt → Node A

1. Node A computes metadata shard = hash("foo.txt") % numShards
2. Node A computes shard owner = consistentHash(shard, clusterMap)
3. Node A forwards metadata write to shard owner (Node B)
4. Node B writes to local BoltDB + replicates to M-1 metadata replicas
5. Node B returns ACK to Node A
6. Node A writes blob data to CRUSH placement targets (data replication)
7. Node A returns success to client
```

