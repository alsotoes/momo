# Tasks: R9 — Secrets Management + Key Rotation

## Implementation

### External Secret Sources
- [ ] `src/common/config.go`: Add `SecretsConfig` with `Source`, `Sources[]`, `VaultConfig`, `AWSConfig`, `GCPConfig`, `AzureConfig`
- [ ] `src/common/secrets.go`: New file — `SecretProvider` interface + implementations (`FileProvider`, `EnvProvider`, `VaultProvider`, `AWSProvider`, `GCPProvider`, `AzureProvider`)
- [ ] `src/common/config.go`: Parse `[secrets]` section; instantiate provider chain

### Versioned Key Store
- [ ] `src/common/keyregistry.go`: New file — `KeyRegistry` with BoltDB bucket `key_registry`
- [ ] `KeyRegistry`: `StoreKey(purpose, material) (keyID string)`, `GetActiveKey(purpose) (keyID, material)`, `MarkRetired(keyID)`, `ListVersions(purpose)`
- [ ] Schema: `key_id` → `{version, material, created_at, rotated_at, algorithm, status, purpose}`

### Rotation Mechanism
- [ ] `src/common/rotation.go`: `RotationManager` with `Start()` (ticker), `Rotate(purpose)`, `Reload()`
- [ ] `RotationManager`: On interval, call `GenerateNewKey(purpose)`, store as `pending_rotation`, signal reload
- [ ] `src/common/secrets.go`: Add `Reload()` to all providers; hot-swap active key
- [ ] `src/momo.go`: Add `handleSIGHUP()` → `rotationMgr.Reload()`; HTTP `/reload-secrets` endpoint
- [ ] `src/momo.go`: Add `rotate-secrets` CLI command (`--purpose <encryption|auth|e2ee|oprf>`)
- [ ] Grace period: after rotation, keep old key `active` for `[secrets] rotation_grace_period` (default 24h), then `MarkRetired`

### Audit Integration
- [ ] `src/common/log.go`: Add `AuditRotation(purpose, oldKeyID, newKeyID, operator, trigger, success, error)`
- [ ] `rotation.go`: Call `AuditRotation` on every rotation (scheduled/manual)

### Config
- [ ] `src/common/config.go`: Parse `[secrets]` section with all sub-keys
- [ ] `conf/momo.conf`: Document `[secrets]` section with all options

## Testing

- [ ] `TestSecretProviderChain` — verifies fallback order (env → vault → file)
- [ ] `TestKeyRegistryVersioning` — verifies versioning, active/retired status
- [ ] `TestRotationFlow` — verifies scheduled + manual rotation, graceful reload
- [ ] `TestRotationAudit` — verifies audit entries for success/failure
- [ ] `TestGracePeriod` — verifies old key usable during grace period
- [ ] `go test -race ./...` — all tests pass
- [ ] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #937` (Rule 11)