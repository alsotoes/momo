# GDPR Compliance

## 6. GDPR Compliance

### 6.1 Right to Erasure (Article 17)

- **Per-tenant delete**: Remove all objects, metadata, tombstones, audit logs for a tenant
- **Per-object delete**: Delete object + all replicas + all metadata shards + tombstone with `MaxRetentionDays=0`
- **Crypto-shredding**: Delete encryption key → data becomes unreadable even if physical deletion fails
- **Verification**: Scrub thread verifies all copies (data + metadata + replicas + backups) are removed

### 6.2 Right to Portability (Article 20)

- **Export API**: Stream all tenant objects + metadata in standard format (S3-compatible listing + tar stream)
- **Metadata export**: JSON dump of all namespace mappings, paths, custom metadata
- **No vendor lock-in**: S3-compatible API ensures standard tooling can consume exported data

### 6.3 Data Residency (Article 44)

- **Region pinning**: Per-tenant region constraint — CRUSH placement restricted to nodes in the allowed region
- **Cluster map tagging**: Each node tagged with `region` and `zone`; CRUSH respects residency rules
- **Audit**: All access logged with node region for compliance verification

### 6.4 Encryption at Rest (Article 32)

- **Per-tenant KMS**: Each tenant has a unique encryption key managed by an external KMS (Vault, AWS KMS, etc.)
- **Envelope encryption**: Blob encrypted with data key, data key encrypted with tenant master key
- **Key rotation**: Automatic rotation per tenant policy; re-encryption is a background scrub job

