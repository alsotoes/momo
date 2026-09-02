# Design Principles & Vision
# Architecture Decision Records

Architectural decisions are documented as **Architecture Decision Records (ADRs)** in `docs/adr/`. Each ADR corresponds to an OpenSpec change under `openspec/changes/`. See `docs/adr/README.md` for the index and process.

---


### 2.1 Design Principles

1. **Read From Any Node** — Any node can serve any read request. The client never needs to know which node holds the data. The node receiving the request resolves metadata, serves locally if it has the data, or transparently proxies from the node that does. This is the single most important principle — it makes the cluster appear as one filesystem to the client.
2. **Zero Single Point of Failure** — No coordinator, no metadata server, no master. Every node can serve every request.
3. **Customer-Transparent Complexity** — Sharding, replication, healing, tiering, erasure coding, metadata placement — all invisible to the customer. They see a filesystem or an S3 endpoint. Nothing else. No client-side cluster awareness required.
4. **Multi-Scheme Replication** — Configurable per-tenant or per-bucket: chain, splay, erasure coding, replica-on-write.
5. **Self-Healing** — Background scrub threads detect and repair inconsistencies, bitrot, under-replication, and orphaned data. The customer never sees a corrupted file or a missing replica.
6. **Massively Scalable** — Horizontal scale to thousands of nodes. Consistent hashing for data and metadata distribution. No global locks.
7. **Multi-Backend** — Local, NFS, S3, raw device, Ceph RADOS, and tiered (hot/cold) storage.
8. **Multi-Tenant** — Isolated namespaces, per-tenant auth, quotas, billing, and encryption keys.
9. **GDPR Compliant** — Right to erasure, right to portability, data residency, audit trail, encryption at rest.
10. **AI-Ready** — Vector embeddings, semantic search, content classification, intelligent tiering.
11. **Fast Search** — Inverted index, bloom filters, vector index (ANN), metadata search.
12. **Fast Recovery** — Journaling, write-ahead logs, parallel rebuild, incremental sync.
13. **HPC Ready** — Parallel I/O, POSIX semantics (optional), MPI-friendly, high throughput for large files, concurrent reads from thousands of processes.
14. **Cloud Ready** — S3-compatible API, elastic auto-scaling, Kubernetes CSI driver, container-native, pay-per-use billing, REST API.

---

## 2.2 Read From Any Node — The Core Principle

This is what transforms Momo from a "replicated blob store" into a "distributed filesystem." Without this, the client must know which node has the data. With this, the cluster is a single filesystem.

### How It Works

```
Client reads /tenant/photos/sunset.jpg
          │
          ▼
    ┌─────────────┐
    │  Node C     │  (receives request — may or may not have the data)
    │  (any node) │
    └──────┬──────┘
           │
           │  Step 1: Resolve metadata
           │  shard = consistentHash("/tenant/photos/sunset.jpg")
           │  shard owner = Node A
           │
           ▼
    ┌─────────────┐
    │  Node A     │  (metadata shard owner)
    │  returns:   │
    │  hash=abc.. │
    │  size=2MB   │
    │  replicas=  │
    │  [B,D,E]    │
    └──────┬──────┘
           │
           │  Step 2: Node C checks — do I have blob abc.. locally?
           │
           ├── YES → Serve directly from local storage (fastest path)
           │
           └── NO  → Stream from Node B (or D, or E — pick fastest/closest)
                     │
                     ▼
               ┌─────────────┐
               │  Node B     │  (has the blob)
               │  streams    │
               │  blob data  │
               └──────┬──────┘
                      │
                      ▼
                 Client receives data
                 (never knew it came from Node B via Node C)
```

### Why This Matters

| Without "Read From Any" | With "Read From Any" |
|------------------------|---------------------|
| Client must know which node has the file | Client talks to any node — any endpoint works |
| Load balancer must be metadata-aware | Any round-robin load balancer works |
| Node failure = client must retry on another node | Node failure = cluster transparently routes to a replica |
| Scaling reads requires directing clients to the right node | Scaling reads = add nodes, any node can serve any read |
| HPC jobs must pin to nodes with data | HPC jobs run anywhere — data finds the compute |
| Cloud deployments need a smart gateway | Cloud deployments: any node is a gateway |

### Read Path Optimization

1. **Local hit** (node has the blob): Serve directly — sub-millisecond for NVMe, single-digit ms for SSD
2. **Local miss, remote hit**: Stream from replica node — add network RTT only
3. **Replica selection**: Pick the replica with lowest RTT (tracked via P2P gossip RTT metrics)
4. **Read repair**: If a replica is found corrupt during read, trigger repair in background, serve from healthy replica
5. **Caching**: Node C caches recently-proxied blobs in a local LRU cache — subsequent reads of the same blob are local hits
6. **Parallel reads**: For large files, stream from multiple replicas simultaneously (striped reads)

---

## 2.3 HPC Ready

### Parallel I/O

- **Concurrent reads**: Thousands of HPC processes read the same file simultaneously — any node can serve, load is spread across all replicas
- **Striped reads**: Large files are striped across nodes — parallel reads from multiple nodes maximize bandwidth
- **MPI integration**: MPI-IO adapter reads from any node — no need for a shared filesystem mount
- **POSIX semantics (optional)**: FUSE mount provides POSIX filesystem interface for legacy HPC applications
- **No data pinning**: HPC scheduler places jobs on any node — data is always accessible

### Performance Targets

| Metric | Target | How |
|--------|--------|-----|
| Read latency (local hit) | < 1ms | NVMe + BoltDB metadata lookup |
| Read latency (remote) | < 5ms | Network RTT + blob stream |
| Aggregate read bandwidth | N_nodes × disk_bandwidth | Parallel reads from all replicas |
| Metadata lookup | < 1ms | BoltDB local lookup (shard owner has it locally) |
| Large file read (1TB) | Minutes, not hours | Striped parallel reads from all replica nodes |
| Concurrent clients | 10,000+ | Connection pooling + semaphore (already 1000, scale with config) |

---

## 2.4 Cloud Ready

### S3-Compatible API (Already Exists)

- Standard S3 REST: `GET /`, `GET /key`, `PUT /key`, `DELETE /key`
- Works with aws-cli, s3cmd, boto3, MinIO client, rclone
- Any node is a valid S3 endpoint — no dedicated gateway needed

### Cloud-Native Deployment

- **Kubernetes**: CSI driver for pod-mounted volumes, StatefulSet for Momo nodes
- **Docker**: Single binary, no external dependencies, container-ready
- **Auto-scaling**: Add nodes to scale — cluster auto-rebalances shards and data
- **Pay-per-use**: Per-tenant billing based on storage + bandwidth + requests
- **Multi-region**: Cross-cluster replication for disaster recovery and data residency

### Cloud Architecture

```
                    ┌─────────────────┐
                    │  Load Balancer  │  (any round-robin — no sticky sessions needed)
                    │  (AWS ALB,      │
                    │   HAProxy, etc) │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
         ┌─────────┐   ┌─────────┐   ┌─────────┐
         │ Node A  │   │ Node B  │   │ Node C  │
         │ (S3 API)│   │ (S3 API)│   │ (S3 API)│
         │ + data  │   │ + data  │   │ + data  │
         │ + meta  │   │ + meta  │   │ + meta  │
         └─────────┘   └─────────┘   └─────────┘
              │              │              │
              └──────────────┴──────────────┘
                             │
                    ┌────────┴────────┐
                    │  Shared Blob     │  (optional: S3, NFS — or local-only)
                    │  Backend         │
                    └─────────────────┘
```

**Key insight**: The load balancer doesn't need to be smart. Any node can handle any request. This is what makes it truly cloud-ready — no special routing, no metadata-aware gateway, no sticky sessions.

---
