# 0101-secrets-rotation-honest-posture

## Status
Proposed

## Confidence
High

## Context
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

## Decision
- Rotation Refuses While Unwired: A rotation request for any known purpose MUST fail with `ErrRotationNotWired` wrapped by `syscall.ENOTSUP`. The refusal MUST be audited with `success=false` and a reason that names the missing data-path wiring. No key material may be generated, stored, promoted or demoted, and no success hook may fire.
- Aggregate Rotation Propagates Failure: `RotateAll` MUST return the first per-purpose failure so callers cannot mistake a refusal for a completed rotation.
- Reload Reports Provider Failures: `POST /reload-secrets` MUST return `500` when the provider reload fails, and `200` only when it succeeded. It MUST NOT be presented as a rotation: it records the current provider secret versions in the registry and does not change the key in use.
- CLI States the Refusal: `momo -imp rotate-secrets` MUST exit as a crypto error with a message that rotation is not wired, and MUST NOT contact the metrics endpoint to perform a reload under a rotation name.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/074-when-rotation-does-nothing.md

## References
- Issue: #1329
- PR: 
- Spec: `openspec/changes/secrets-rotation-honest-posture/`
- Blog: docs/blog/posts/074-when-rotation-does-nothing.md

