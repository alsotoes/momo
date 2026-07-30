# Scrub & Self-Healing

## 4. Scrub & Self-Healing

### 4.1 Current State

- **GC**: Local-only reference counting. Sweeps orphaned blobs (RefCount=0) and expired tombstones. No cross-node verification.
- **No scrub threads**: No background integrity checking, no replica verification, no bitrot detection.

### 4.2 Target: Multi-Layer Scrub

#### Shallow Scrub (frequent — every 5 min)
- Verify each local blob's existence on disk/S3
- Check replica count: does the object have the expected number of replicas?
- Flag under-replicated objects for re-replication
- Check metadata consistency: does every namespace entry point to a valid object?

#### Deep Scrub (infrequent — daily)
- Read every blob and verify SHA-256 hash matches the key (bitrot detection)
- Compare replicas across nodes: do all replicas have identical content?
- Verify metadata shard replicas are consistent
- Check for orphaned blobs (exist on disk but no metadata entry)
- Check for orphaned metadata (entry exists but blob missing)

#### Repair (automatic)
- **Under-replicated**: Select new target nodes via CRUSH, replicate blob
- **Bitrot detected**: Replace corrupt replica from a healthy one
- **Orphaned blob**: Add to metadata or delete (configurable)
- **Missing metadata**: Reconstruct from other metadata shard replicas
- **Split-brain metadata**: Use last-writer-wins with vector clocks, log conflict

#### Scrub Thread Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    ScrubCoordinator                        │
│  (runs on every node, coordinates local scrub)            │
├──────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │ ShallowScrub │  │  DeepScrub   │  │  RepairQueue  │    │
│  │  (5 min)     │  │  (daily)     │  │  (continuous) │    │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘    │
│         │                  │                  │            │
│         ▼                  ▼                  ▼            │
│  ┌──────────────────────────────────────────────────┐    │
│  │              Scrub Report Queue                    │    │
│  │  (persisted to BoltDB scrub bucket, durable)      │    │
│  └──────────────────────────────────────────────────┘    │
│                          │                                │
│                          ▼                                │
│  ┌──────────────────────────────────────────────────┐    │
│  │              Repair Worker Pool                    │    │
│  │  (bounded goroutines, rate-limited, retryable)    │    │
│  └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────┘
```

