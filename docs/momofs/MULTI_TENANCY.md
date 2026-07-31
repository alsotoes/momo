# Multi-Tenancy

## 5. Multi-Tenancy

### 5.1 Current State

- Single global `auth_token` for all clients
- No tenant isolation, no per-tenant namespaces
- No quotas, no billing, no per-tenant encryption

### 5.2 Target: Full Multi-Tenancy

#### Tenant Model

```
Tenant
├── ID: UUID
├── API Keys: [key1, key2, ...]  (per-tenant auth)
├── Namespace Root: /tenant-{id}/
├── Quotas:
│   ├── MaxStorageBytes
│   ├── MaxObjectCount
│   ├── MaxBandwidthPerSec
│   └── MaxRequestsPerSec
├── Encryption:
│   ├── KMS Key ID (per-tenant encryption at rest)
│   └── TLS cert pinning (optional)
├── Replication Policy: (per-tenant override)
│   ├── Strategy: chain | splay | erasure
│   └── Factor: 3
├── Tiering Policy:
│   ├── Hot tier: NVMe (0-30 days)
│   ├── Warm tier: SSD (30-90 days)
│   └── Cold tier: S3/Glacier (90+ days)
├── Retention Policy:
│   ├── MinRetentionDays
│   ├── MaxRetentionDays
│   └── LegalHolds: [object1, object2, ...]
└── Audit Log: (append-only, GDPR compliant)
    ├── Who accessed what, when, from where
    └── Retention: 90 days (configurable)
```

#### BoltDB Schema Changes

| Bucket | Key Change | Value Change |
|--------|-----------|--------------|
| `namespace` | `tenantID:name` | unchanged |
| `objects` | unchanged (content-addressed) | add `TenantIDs []UUID` (which tenants reference this blob) |
| `paths` | `tenantID:name` | unchanged |
| `tombstones` | `tenantID:name` | unchanged |
| `tenants` (new) | `tenantID` | tenant config (JSON) |
| `quotas` (new) | `tenantID` | `{UsedBytes, UsedObjects, BandwidthUsed}` |
| `audit` (new) | `tenantID:timestamp:op` | audit entry (JSON) |

