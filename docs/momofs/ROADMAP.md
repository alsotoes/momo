# Implementation Roadmap, BoltDB Evolution & SPOF Checklist

## 12. Implementation Roadmap

### Phase 1: Foundation (Current → Distributed Metadata)
- [ ] Metadata partitioning via consistent hashing
- [ ] Metadata replication (separate from data replication)
- [ ] Cluster-wide List via scatter-gather
- [ ] Vector clocks for metadata versioning
- [ ] Merkle tree for replica divergence detection

### Phase 2: Self-Healing
- [ ] Shallow scrub thread (replica count, existence check)
- [ ] Deep scrub thread (bitrot, cross-replica comparison)
- [ ] Repair queue + worker pool
- [ ] Under-replication detection and auto-repair
- [ ] Orphan cleanup (cross-node)

### Phase 3: Multi-Tenancy
- [ ] Tenant model + per-tenant auth (API keys)
- [ ] Tenant-aware BoltDB keys
- [ ] Per-tenant quotas and rate limiting
- [ ] Per-tenant encryption (KMS integration)
- [ ] Audit log per tenant

### Phase 4: GDPR
- [ ] Right to erasure (tenant delete, object delete + all replicas)
- [ ] Crypto-shredding support
- [ ] Data residency (region pinning in CRUSH)
- [ ] Right to portability (export API)
- [ ] Legal holds

### Phase 5: AI-Ready
- [ ] Vector embeddings on ingest (configurable per tenant)
- [ ] HNSW index for semantic search
- [ ] Content classification (PII detection, content type)
- [ ] Intelligent tiering (access pattern tracking + ML)
- [ ] Semantic deduplication

### Phase 6: Fast Search & Recovery
- [ ] Inverted index for full-text search
- [ ] Bloom filters for fast Has() across cluster
- [ ] Write-ahead log (WAL) for metadata
- [ ] Incremental sync via Merkle trees
- [ ] Parallel shard recovery on node rejoin

### Phase 7: Erasure Coding
- [ ] Reed-Solomon implementation
- [ ] Erasure coding as replication mode 3
- [ ] Rack/zone-aware chunk placement
- [ ] Parallel chunk reconstruction for reads

### Phase 8: Scale
- [ ] Tiered storage (hot/warm/cold auto-tiering)
- [ ] Cluster expansion (add nodes, rebalance shards)
- [ ] Cluster contraction (remove nodes, evacuate data)
- [ ] Cross-cluster replication (geo-replication for DR)

---

## 13. BoltDB Evolution Summary

### Current Buckets (Local Only)

| Bucket | Key | Value | Scope |
|--------|-----|-------|-------|
| `objects` | content hash | 24B ObjectMeta | Local |
| `namespace` | full virtual path | content hash | Local |
| `paths` | full virtual path | remote path | Local |
| `tombstones` | full virtual path | 8B timestamp | Local (+P2P sync) |
| `raw_alloc` | content hash | 16B [offset][len] | Local |

### Target Buckets (Distributed + Multi-Tenant)

| Bucket | Key | Value | Scope | Phase |
|--------|-----|-------|-------|-------|
| `objects` | content hash | ObjectMeta + TenantIDs + VectorClock | Distributed (sharded + replicated) | 1 |
| `namespace` | `tenantID:path` | content hash | Distributed (sharded + replicated) | 1, 3 |
| `paths` | `tenantID:name` | normalized path | Distributed (sharded + replicated) | 1, 3 |
| `tombstones` | `tenantID:name` | 8B timestamp + VectorClock | Distributed (sharded + replicated) | 1, 3 |
| `raw_alloc` | content hash | 16B [offset][len] | Local (raw backend only) | — |
| `tenants` | tenantID | tenant config (JSON) | Distributed (full replica on all nodes) | 3 |
| `quotas` | tenantID | quota usage counters | Distributed (sharded) | 3 |
| `audit` | `tenantID:ts:op` | audit entry (JSON) | Distributed (sharded, TTL-expired) | 3, 4 |
| `embeddings` | content hash | float32[] vector | Distributed (sharded + replicated) | 5 |
| `classifications` | content hash | JSON classification | Distributed (sharded + replicated) | 5 |
| `access_stats` | content hash | access pattern stats | Distributed (sharded) | 5 |
| `ann_index` | shard ID | HNSW graph snapshot | Distributed (sharded) | 5 |
| `wal` | sequence number | WAL entry | Distributed (replicated) | 6 |
| `scrub_reports` | `ts:shard:type` | scrub findings (JSON) | Local | 2 |
| `merkle_roots` | shard ID | Merkle root hash | Distributed (gossip-exchanged) | 1, 6 |

---

## 14. No Single Point of Failure Checklist

| Requirement | Current | Target | How |
|-------------|---------|--------|-----|
| No metadata server | Local BoltDB (no sharing) | Distributed sharded BoltDB | Consistent hashing + metadata replication |
| No coordinator | CRUSH (data placement) | CRUSH (data + metadata placement) | Extend CRUSH to metadata shards |
| No master node | P2P gossip membership | Same | Already satisfied |
| No shared storage requirement | Each node has local storage | Same | Blob backends can be local or shared |
| No external dependency for metadata | BoltDB (embedded) | BoltDB (embedded) | No etcd/consul needed — metadata in BoltDB |
| Tolerates node failure | Data replication (N copies) | Data + metadata replication | Separate replication factors for data and metadata |
| Tolerates network partition | Partial (gossip continues) | Lease-based quorum for writes | Quorum = (metadataReplicas/2)+1 |
| Tolerates disk failure | No (local disk = data loss) | Data + metadata replicas on different nodes | CRUSH rack/zone awareness |

---

## References

- [ARCHITECTURE.md](ARCHITECTURE.md) — Current system architecture
- [REPLICATION_STRATEGIES.md](REPLICATION_STRATEGIES.md) — Chain and Splay replication
- [P2P.md](P2P.md) — Gossip membership, SWIM, lease consensus
- [CRUSH.md](CRUSH.md) — Data placement algorithm
- [ROADMAP.md](ROADMAP.md) — Existing roadmap
