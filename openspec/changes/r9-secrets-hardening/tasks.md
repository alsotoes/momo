# Tasks: R9 secrets-management hardening + SonarQube gate

## Implementation

- [x] Fix `RotationManager.Rotate` to store generated material, activate the new key, and report real key IDs (`src/common/rotation.go`)
- [x] Fix `loadSecretsConfig` to read `[secrets.<source>]` child sections; decompose into helpers to cut cognitive complexity (`src/common/config.go`)
- [x] Initialize R9 before `StartMetricsServer`; extract `initSecretsManager` (`src/server/server.go`)
- [x] Add Rule 37 panic recovery to the scheduler goroutine and rotation methods; map failures to `syscall` constants
- [x] Hoist the purpose list to a package-level `rotationPurposes`
- [x] Correct the invalid documented duration `90d` to `2160h` (`conf/momo.conf`, `docs/GUIDES/CONFIGURATION.md`)

## Testing

- [x] Key registry, rotation manager, provider chain, and config loading unit tests
- [x] Audit-log and `/reload-secrets` endpoint tests
- [x] `initSecretsManager` tests (disabled, enabled, bad provider, unopenable DB)
- [x] `go build ./...`, `go vet`, `gofmt`, `go test ./src/...` clean
- [x] SonarQube Quality Gate passes (≥80% new-code coverage, 0 new issues)

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] PR links tracking issue #1153 (Rule 11)
