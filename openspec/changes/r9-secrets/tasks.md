# Tasks: R9 — Secrets Management + Key Rotation

## Implementation

### Secret Providers
- [x] `src/common/config.go` / `struct.go`: `SecretsConfig` with `Source`, `Sources[]`, per-source config
- [x] `src/common/secrets.go`: `SecretProvider` interface + `EnvProvider` + `FileProvider`
- [x] `src/common/secrets.go`: `ProviderChain` fallback (env → file); `BuildProviderChain`
- [x] `src/common/config.go`: Parse `[secrets]` section; instantiate the provider chain

### Key Registry
- [x] `src/common/keyregistry.go`: `KeyRegistry` over the `key_registry` BoltDB bucket
- [x] `StoreKey` / `GetActiveKey` / `GetKey` / `SetActive` / `MarkRetired` / `MarkCompromised` / `ListVersions`
- [x] Schema: `{version, material, status, algorithm, tenant, timestamps}` with statuses active/retired/pending_rotation/compromised

### Rotation
- [x] `src/common/rotation.go`: `RotationManager` with `Start()` (ticker), `Rotate`, `RotateAll`
- [x] On rotation: generate new material, `StoreKey` pending, `SetActive` (promotes new, retires old)
- [x] Grace period: the previous key is retained (not deleted), so old ciphertext keeps decrypting
- [x] `src/common/secrets.go`: `Reload()` on providers; `fireReloadHooks` hot-swap signal
- [x] `src/momo.go`: `rotate-secrets` CLI command (`--purpose`)
- [x] `src/server/server.go`: `POST /reload-secrets` endpoint
- [x] `src/server/server.go`: `SIGHUP` → `RotationManager.Reload()`

### Audit
- [x] `src/common/log.go`: `AuditRotation(purpose, old, new, operator, trigger, success, err)`
- [x] `rotation.go`: call `AuditRotation` on every rotation (success and failure)

### Config & Docs
- [x] `conf/momo.conf`: document `[secrets]` with all options
- [x] `docs/GUIDES/CONFIGURATION.md`: document `[secrets]`

### Deferred (documented design decision)
- [ ] External providers (Vault, AWS Secrets Manager, GCP Secret Manager, Azure Key Vault).
      The `SecretProvider` seam + `ProviderChain` are designed so these can be added
      as compile-time providers without touching callers; heavy cloud SDKs are
      intentionally out of the default build.

## Testing

- [x] `TestSecretProviderChain` / `TestBuildProviderChain` — fallback order and defaults
- [x] `TestKeyRegistry*` — versioning, active/retired status transitions
- [x] `TestRotationManager_*` — reload, rotate, RotateAll, generate, audit, old-key retention
- [x] `TestRotationManager_AuditRecorded` — rotation writes a chained audit entry
- [x] `go test -race ./...` — all tests pass
- [x] `make test` — full suite passes

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rules 77/78)
- [x] Blog post shipped — `054-secrets-management-key-rotation.md` (Rule 76)
- [ ] PR body includes `Resolves #937` (Rule 11)
