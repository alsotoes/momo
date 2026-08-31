# Object Pinning

## Change ID
object-pinning-v1

## Status
Ratched

## Summary
Add a `Pinned` flag to `ObjectMeta` that protects blobs from GC deletion even when refcount drops to zero. Critical for compliance, legal holds, system data, and "important" user data.

## Motivation
Current MomoFS GC deletes blobs when refcount=0 (after Delete or expiration). No way to protect "critical" data: legal holds, compliance requirements, system configuration, pinned cache warm objects. IPFS uses pinning similarly. This change adds first-class pinning support.

## Motivation Details
- Legal holds: regulated data must not be deleted per compliance
- System data: cluster config, metadata, config blobs must survive cluster teardown
- Compliance: GDPR Article 17 (right to erasure) has exceptions for legal hold
- Cache warming: pin hot edge-cache objects to prevent eviction
- User expectations: "important photos" shouldn't vanish when refcount hits 0

## Owner
@alsotoes

## Dependencies
- BoltDB ObjectMeta extension with Pinned bool field
- GC loop modification (check Pinned flag before deletion)
- API: Pin/Unpin operations (per-object, per-bucket, per-tenant)
- Documentation: pin management UI/CLI

## Caveats
- Pinned blobs still count toward storage quota (configurable)
- Pinned blobs still replicated via normal replication protocol
- Pinned blobs still subject to cluster rebalancing (replicas re-distributed)
- Pinned flag persists across cluster restarts (stored in BoltDB)
- API: separate "pin" command from "delete" — user must explicitly unpin before delete

## Technical Details

### ObjectMeta Extension
```go
type ObjectMeta struct {
    Size       int64
    RefCount   int64
    DeletedAt  int64
    Checksum   uint32      // CRC32C of blob content
    Pinned     bool        // if true, GC never deletes this blob
    InlineData []byte      // present if Size <= inlineThreshold (e.g., 4KB)
    Replicas   []int       // data node IDs from CRUSH
    VectorClock []uint64   // for conflict resolution
    ShardKey   string      // consistent hash ring shard key
}
```

### GC Modification
```go
func (s *GC) Run() {
    // Existing: scan objects bucket, remove RefCount=0
    // New: also check Pinned flag
    
    for _, obj := range objectsBucket.Values() {
        if obj.RefCount <= 0 && !obj.Pinned {
            // Delete: remove from namespace, objects, paths, set tombstone
            DeleteObject(obj.Key)
        }
        // if PinNED: skip deletion, keep indefinitely (or until Unpin)
    }
}
```

### API: Pin/Unpin Operations

#### Pin Object
```
POST /_pin?key=filename.txt
OR
PUT /_pin?keys=filename1.txt,filename2.txt  (batch)

Response: 202 Accepted, pin applied
```

#### Unpin Object
```
DELETE /_pin?key=filename.txt

Response: 202 Accepted, pin removed
```

#### Pin Bucket
```
PUT /_bucket/photos/_pin
```
Pins all objects in bucket "photos".

#### Pin Tenant
```
PUT /_tenant/acme/_pin
```
Pins all objects in tenant "acme" bucket.

### API Examples

```go
// Pin a critical file
err := client.Pin("legal-hold-doc.txt")

// Pin all files in a bucket
err := client.PinBucket("photos")

// Pin all files for a tenant  
err := client.PinTenant("acme-corp")

// Unpin a file (then delete)
err := client.Unpin("temp-file.txt")
err := client.Delete("temp-file.txt")
```

### Configuration

```toml
[momofs]
pin_protected = true  # GC respects Pinned flag (default true)
pin_quota_mb = -1     # -1 = unlimited, else limit pinned data size
pin_default = false   # new objects not pinned by default
```

### GC Interaction

```
GC Loop (run periodically):
1. Scan objects bucket: key → {size, refCount, deletedAt, pinned}
2. If refCount <= 0 AND !pinned → delete (existing behavior)
3. If refCount <= 0 AND pinned → skip deletion, keep blob
4. Log: "skipped GC for pinned blob: key=X, reason=protection"
5. Optional: pin expiry — if pinned for > N days, auto-unpin (audit log)

# Example config for auto-expiry:
pin_max_age_hours = 168   # 7 days max pin lifetime
pin_auto_expiry = true   # auto-unpin after max age
```

### Failure Scenarios

#### Pin Protects Against Accidental Delete
```
User: Delete "important-doc.txt"
→ System: "Cannot delete: blob is pinned. Unpin first."
→ User: Unpin "important-doc.txt"
→ User: Delete "important-doc.txt" → succeeds → blob GC'd

OR
→ User: Unpin "important-doc.txt" → immediately deleted (if auto-deletes)
```

#### Pin Protects Against GC After RefCount=0
```
1. Put "important-doc.txt" → refCount=1
2. Delete "important-doc.txt" → refCount→0, but Pinned=true → GC skips
3. Later: refCount still 0 (nobody references it)
4. GC runs: sees Pinned=true → skips deletion
5. Blob remains in cluster indefinitely (or until Unpin)
6. Cluster rebalance: blob moves with its replicas (still pinned)
```

#### Multiple Pins / Unpin Race
```
Node A: Pin "file.txt" → pin count = 1
Node B: Unpin "file.txt" → pin count = 0 → GC can delete
Node C: Pin "file.txt" → pin count = 1 → GC skips again
```

Solution: pin count (int), not just bool. Unpin only deletes when pin count = 0.

### Storage Quota Impact

```
Pinned blobs still count toward:
- Per-node storage quota (MB/GB configured)
- Per-tenant quota (if tenant-based quotas)
- Cluster total usage

Optionally:
pin_count_toward_quota = true  # default: pinned data counts toward quota
pin_count_toward_quota = false # optional: exempt pinned data from quota

Use case: legal hold data exempt from storage quotas (compliance requirement)
```

### Migration

**No migration needed**: Pinned flag defaults to `false` for all existing objects. New objects can be pinned via API. No data movement required.

**Opt-in path**:
1. Upgrade to new daemon version
2. Existing objects: Pinned=false (GC behaves as before)
3. New objects: pin via API or API-based migration
4. Critical data: manually pin important blobs

### Testing Strategy

1. **Unit tests**: GC loop with Pinned=true/false, verify deletion behavior
2. **API tests**: Pin/Unpin operations, verify flag set/cleared
3. **GC tests**: Run GC loop, verify pinned blobs survive, non-pinned deleted
4. **Concurrent**: Simultaneous Pin/Unpin/GC, no race conditions
5. **Quota**: Pinned data counts/exempt from quota as configured

### Acceptance Criteria
- [ ] Pinned flag defaults to false for all existing objects
- [ ] GC skips deletion when Pinned=true and refCount=0
- [ ] API: Pin object → flag set in BoltDB
- [ ] API: Unpin object → flag cleared
- [ ] API: Pin bucket → all objects in bucket pinned
- [ ] API: Pin tenant → all objects in tenant pinned
- [ ] Unpin → allows GC deletion (unless other pins exist)
- [ ] S3 API → Pin/Unpin operations work (internal, invisible to S3 client)
- [ ] Configuration: pin_protected flag respected by GC
- [ ] Configuration: pin_quota_mb adjustable
- [ ] Migration: no data movement required, defaults to false

### Open Questions / Open Issues
1. Pin count vs bool: should we track pin count (int) or just presence (bool)?
   - Bool simpler; count more granular (allows "pin once, unpin once")
   - Recommendation: start with bool, add count if use case requires
2. Pin expiry: should pins auto-expire? (compliance may require permanent pin)
   - Recommendation: optional config, default disabled (manual unpin required)
3. Pin + versioning: if object versioned, does pin apply to all versions or current only?
   - Recommendation: pin applies to current version chain (all versions protected)
4. Pin + rebalancing: during cluster rebalance, are pinned blobs re-replicated?
   - Recommendation: yes, pinned blobs treated like any other blob for replication

### Linked GitHub Issues
- Issue #XXX: Object pinning feature for compliance/legal holds
- Issue #YYY: GC modification for Pinned flag
- Issue #ZZW: Pin/Unpin API design