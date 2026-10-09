# Change: R9 secrets-management hardening + SonarQube gate

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1153 (tracking)

## Why

The initial R9 secrets-management delivery (`46b53132`, #937) failed the
SonarQube quality gate (new-code coverage 38.9%, Minor vulnerabilities,
Critical code smells). Investigation surfaced real defects beyond the missing
tests: `RotationManager.Rotate` always failed because it stored empty key
material, `loadSecretsConfig` never read per-source child sections, and the
`/reload-secrets` endpoint was wired before its rotation manager existed, so it
was a permanent no-op.

## What Changes

- Fix `RotationManager.Rotate` to store the generated material, activate the
  new key, signal reload, and report real old/new key IDs to audit hooks.
- Fix `loadSecretsConfig` to read `[secrets.<source>]` child sections, and keep
  the scalar parsing decomposed (`loadSecretsBase`, `parseSecretSources`,
  `loadSourceConfig`, `fillSourceConfig`) to stay within cognitive-complexity
  limits.
- Initialize R9 before `StartMetricsServer` so `/reload-secrets` is functional;
  extract `initSecretsManager` for direct testing.
- Add Rule 37 panic recovery to the rotation scheduler goroutine and the
  rotation manager methods, and map validation/lookup failures to POSIX
  `syscall` constants (Rule 10/42). Hoist the fixed purpose list to a
  package-level variable (Rule 19/Bolt).
- Raise new-code coverage above the 80% gate with dedicated tests for the key
  registry, rotation manager, providers, config loading, audit log, and the
  metrics reload endpoint.
