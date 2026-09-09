# 0062-r9-secrets

## Status
Proposed

## Confidence
Low

## Context
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

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #937
- PR: 
- Spec: `openspec/changes/r9-secrets/`
- Blog: 

