# Change: R9 — Secrets Management + Key Rotation

**Related Issues:**
- https://github.com/alsotoes/momo/issues/937 (R9: Secrets management + key rotation)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

All secrets in Momo are currently static config-file values:
- Root `encryption_key` (master KEK for envelope encryption)
- `auth_token` (shared across all clients/servers)
- `e2ee_key` / `e2ee_key_id` (E2EE master keys)
- OPRF shares (for S3 gateway and P2P)

No rotation mechanism exists. If a key is compromised, there is no way to rotate without full cluster restart and manual config update. Production requires:

1. **External secret sourcing** — env vars, HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager, Azure Key Vault
2. **Automated rotation** — scheduled rotation with graceful reload (no restart)
3. **Versioned key storage** — old keys retained for decryption of existing data during transition
4. **Rotation audit trail** — every rotation logged with old/new key IDs, timestamp, operator

## What Changes

### External Secret Sources
- New `[secrets]` config section with `source` enum: `file`, `env`, `vault`, `aws-sm`, `gcp-sm`, `azure-kv`
- File source: existing behavior (read from config file path)
- Env source: read from `MOMO_<SECRET_NAME>` env vars
- Vault/AWS/GCP/Azure: use respective SDKs with IAM/role-based auth

### Versioned Key Store
- New `key_registry` BoltDB bucket: `key_id` → `{ version, material, created_at, rotated_at, algorithm, status }`
- Key status enum: `active`, `retired`, `compromised`, `pending_rotation`
- Active key per purpose: `encryption`, `auth`, `e2ee`, `oprf`
- Retired keys retained for decryption of existing data (never deleted)

### Rotation Mechanism
- `[secrets] rotation_interval` (e.g., `"90d"`) triggers scheduled rotation
- Rotation flow:
  1. Generate new key material (same algorithm)
  2. Store as new version with status `pending_rotation`
  2. Signal all nodes to hot-reload (SIGHUP or HTTP `/reload-secrets`)
  3. Nodes fetch new active key, keep old for decryption
  4. After grace period (`[secrets] rotation_grace_period`, default `24h`), mark old as `retired`
  4. Update `key_registry` with new active version
- Manual rotation: `momo rotate-secrets --purpose <encryption|auth|e2ee|oprf>`

### Audit Integration
- Every rotation emits `AUDIT:` log with: `purpose`, `old_key_id`, `new_key_id`, `operator`, `trigger` (scheduled/manual)
- Failed rotations logged with error and retry count

## Non-Goals

- Hardware Security Module (HSM) integration (future tier)
- Shamir secret sharing for root KEK (future tier)
- Automatic revocation of compromised keys (manual intervention required)
- Cross-region secret sync (future geo-distribution tier)

## Impact

- **Security:** Eliminates static keys; supports compliance (PCI-DSS, SOC2 rotation requirements)
- **Operations:** Zero-downtime rotation; hot reload via SIGHUP
- **Audit:** Full rotation trail for compliance evidence
- **Config:** New `[secrets]` section in `momo.conf`