# Secrets Rotation: Honest Posture

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1329

## Why

R9 shipped a key registry and a rotation manager, and reported rotation as
delivered. It is not: `KeyRegistry.GetActiveKey` has no caller outside
`rotation.go`, so the daemon keeps the secret it resolved at startup no matter
what the registry says. The old behaviour therefore advertised a security control
that never happened:

- `Rotate` generated material, activated it and audited `success=true`;
- `Rotate("oprf")` stored the literal bytes `oprf-share-placeholder`, labelled
  `AES-256-GCM`;
- `momo -imp rotate-secrets` posted to `/reload-secrets`, which only re-read the
  provider chain, and its error was discarded so a failed reload answered `200`;
- the documentation claimed retired keys "remain available for decryption"
  although nothing decrypts with registry keys.

An operator who believes a key was rotated will not rotate again. That is worse
than a clearly unavailable feature.

## What Changes

- `Rotate` refuses every purpose with `ErrRotationNotWired` (`ENOTSUP`), audits
  the refusal with `success=false`, stores nothing and fires no success hook.
- `RotateAll` returns the first failure instead of `nil`.
- `Start` logs that scheduled rotation is disabled and starts no ticker; the
  ticker, stop channel and scheduler goroutine are deleted.
- `generateKeyForPurpose` is deleted with its OPRF placeholder.
- `/reload-secrets` returns `500` when the provider reload fails.
- `momo -imp rotate-secrets` reports the refusal instead of performing a
  mislabelled reload.
- Provider reload keeps working through `SIGHUP` and `POST /reload-secrets`.

## Impact

- Affected specs: secrets-rotation-honest-posture (new)
- Affected code: `src/common/rotation.go`, `src/server/server.go`,
  `src/server/metrics_exporter.go`, `src/momo.go`
- No change to the secret resolution path: the configured secret is still what
  the daemon uses.
