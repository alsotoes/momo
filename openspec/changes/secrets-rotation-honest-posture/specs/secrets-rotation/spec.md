# Secrets Rotation Honest Posture

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1329

## Purpose

Never report a key rotation that does not change the key in use. While rotation
is not connected to the data path, it must fail loudly and visibly rather than
succeed silently.

## Requirements

### Requirement: Rotation Refuses While Unwired

A rotation request for any known purpose MUST fail with `ErrRotationNotWired`
wrapped by `syscall.ENOTSUP`. The refusal MUST be audited with `success=false`
and a reason that names the missing data-path wiring. No key material may be
generated, stored, promoted or demoted, and no success hook may fire.

#### Scenario: Manual rotation of a known purpose

- **GIVEN** a key registry holding an active `encryption` key
- **WHEN** `Rotate` is called for `encryption`
- **THEN** it returns an error matching `ErrRotationNotWired`
- **AND** the registry still reports the same key as active with status `active`
- **AND** an audit entry records the attempt with `success=false`

#### Scenario: Unknown purpose

- **WHEN** `Rotate` is called with a purpose outside the known set
- **THEN** it returns `syscall.EINVAL` and stores nothing

### Requirement: Aggregate Rotation Propagates Failure

`RotateAll` MUST return the first per-purpose failure so callers cannot mistake
a refusal for a completed rotation.

#### Scenario: Scheduled rotation is disabled

- **GIVEN** a positive `rotation_interval`
- **WHEN** `Start` is called
- **THEN** it logs that rotation is not wired and starts no scheduler
- **AND** no rotation audit entry is written until an operator asks for one

### Requirement: Reload Reports Provider Failures

`POST /reload-secrets` MUST return `500` when the provider reload fails, and
`200` only when it succeeded. It MUST NOT be presented as a rotation: it records
the current provider secret versions in the registry and does not change the key
in use.

#### Scenario: Provider unreachable

- **GIVEN** a provider whose `Reload` returns an error
- **WHEN** the admin endpoint is called
- **THEN** the response is `500`
- **AND** the CLI `rotate-secrets` does not route to this endpoint

### Requirement: CLI States the Refusal

`momo -imp rotate-secrets` MUST exit as a crypto error with a message that
rotation is not wired, and MUST NOT contact the metrics endpoint to perform a
reload under a rotation name.

#### Scenario: Operator runs the command

- **WHEN** `rotate-secrets` is invoked with a valid server ID
- **THEN** the command fails with `ErrRotationNotWired` and `ENOTSUP`
- **AND** no HTTP request is made
