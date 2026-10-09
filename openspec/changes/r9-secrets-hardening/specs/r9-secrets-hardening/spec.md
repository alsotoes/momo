# Spec: R9 secrets-management hardening

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1153

## Requirements

### R9H-A1: Manual rotation activates generated key material

**Requirement:** `Rotate` MUST persist the freshly generated key material,
activate it, and leave the registry with exactly one active key per purpose.

**Scenario: Rotation succeeds for a purpose**

Given a key registry with an active `encryption` key,
When `Rotate(ctx, "encryption")` is called,
Then a new key version is stored with non-empty material,
And the new key becomes the active key,
And the rotation hook is fired with the real old and new key IDs.

### R9H-A2: Per-source secret config is loaded from child sections

**Requirement:** `loadSecretsConfig` MUST read per-source options from
`[secrets.<source>]` child sections (env prefix, file path, vault/cloud
fields), and MUST reject unknown sources and non-Go-duration intervals.

**Scenario: Env and file child sections are parsed**

Given a `[secrets]` section listing `sources = env,file` with `[secrets.env]`
and `[secrets.file]` children,
When the configuration is loaded,
Then the env prefix and file path from the child sections are applied,
And absent children fall back to the `MOMO_` prefix and `conf/momo.conf`.

### R9H-A3: /reload-secrets triggers an initialized rotation manager

**Requirement:** The metrics server MUST only be started after the R9 rotation
manager exists, so the `/reload-secrets` endpoint invokes a non-nil reload
callback.

**Scenario: Reload endpoint invokes the callback**

Given secrets management is enabled,
When the server initializes its metrics endpoint and a client POSTs to
`/reload-secrets`,
Then the reload callback runs,
And a GET request is rejected with HTTP 405.

### R9H-A4: Rotation paths are panic-safe and POSIX-mapped

**Requirement:** The rotation scheduler goroutine and the rotation manager
methods MUST recover from panics (Rule 37) and MUST map configuration and
registry failures to `syscall` constants (Rule 10/42).

**Scenario: A panic in a rotation path degrades to a POSIX error**

Given a rotation path that panics,
When the panic is recovered,
Then an error wrapping `syscall.EIO` is returned (or logged for the scheduler),
And the process continues.

**Scenario: Invalid configuration maps to EINVAL**

Given a `[secrets]` section with an invalid `rotation_interval`,
When the configuration is loaded,
Then the returned error wraps `syscall.EINVAL`.
