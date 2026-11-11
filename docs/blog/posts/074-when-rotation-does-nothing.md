---
title: "When Key Rotation Does Nothing"
date: "2026-11-11T10:15:00Z"
draft: false
post_type: architecture
tags:
  - encryption
  - key-rotation
  - security
  - operations
categories:
  - encryption
summary: "momo had a key registry, a rotation manager, a schedule, an audit trail and a CLI command — and none of it changed the key the daemon used. Rotation reported success every time. A security control that reports success without acting is more dangerous than one that is missing, because the missing one is visible."
artifacts: [{type: spec, path: openspec/changes/secrets-rotation-honest-posture}]
related: ["054-secrets-management-key-rotation", "072-configuration-that-cannot-lie", "071-making-refusals-visible", "073-tenancy-you-can-write-down"]
difficulty: "intermediate"
pattern: "honest-refusal"
teaches:
  - "why a no-op security control is worse than an absent one"
  - "how to make a refusal auditable and testable"
---

Every encryption system eventually answers one operational question: how do you
change the key? The answer is usually "rotate it" — and that is exactly where a
system can lie to you without anyone noticing.

## The Real Problem

momo had every piece of a rotation story. A per-node key registry in BoltDB with
versioned keys, an `active`/`retired`/`pending_rotation`/`compromised` lifecycle,
a scheduled rotation manager, an audit log, a `SIGHUP` handler and a CLI command.
The documentation described online rotation with a grace window, and the tests
passed.

The missing piece was invisible from all of those angles: **nothing read the
registry.** The daemon resolved its secrets at startup, from the configuration
file or the provider chain, and handed those bytes to the storage, auth and
transport code. The registry was a library with no caller in the data path.

The consequences were worse than dead code:

- `Rotate` generated a key, promoted it to `active`, and wrote
  `success=true` to the audit log. The key in use did not change.
- The OPRF branch stored the literal string `oprf-share-placeholder` as the new
  active share.
- `momo -imp rotate-secrets` posted to `/reload-secrets`, which only re-read the
  provider chain — and the handler discarded its error, so a failed reload
  answered `200 OK`.
- Retired keys were documented as "available for decryption", but no decryption
  path ever asked the registry for a key version.

An operator who rotates on schedule and reads a success audit entry will not
rotate again. That is the real damage: the control reports green while the
exposure it was meant to bound keeps growing.

## Why the Obvious Solutions Failed

**Wire it quickly.** The tempting fix is a few lines in the write path: read the
active key from the registry, use it. But rotating an at-rest encryption key is
not a metadata swap. Data already written is encrypted with the old key; if the
new one becomes the only key in use, that data is gone. A real implementation
needs envelope encryption — a key-encryption key that wraps per-object data
keys — so rotation re-wraps small keys instead of rewriting terabytes.

**Rotate the auth token too.** A symmetric token is shared by every node and
client. Rotating it on one node breaks the cluster until every participant has
the new value, which means a coordinated rollout, not a local timer.

**OPRF shares.** Those are Shamir shares of a distributed key. Changing one
requires re-sharing with the peers that hold the others.

And a scheduled rotation that runs before the design exists does not just fail
to help — it creates a second problem, because every interval it would log a
failure, and a failing timer is noisy enough that operators learn to ignore it.

## The Solution

The change is deliberately small: the code stops claiming what it does not do.

`Rotate` now refuses every purpose with a named sentinel and a POSIX error:

```go
var ErrRotationNotWired = errors.New("key rotation is not wired to the data path")

func (rm *RotationManager) Rotate(ctx context.Context, purpose string) error {
	if !isRotationPurpose(purpose) {
		return fmt.Errorf("unknown rotation purpose %q: %w", purpose, syscall.EINVAL)
	}
	err := fmt.Errorf("%w: %s: %w", ErrRotationNotWired, rotationNotWiredReason, syscall.ENOTSUP)
	rm.auditRotation(ctx, AuditRotationEntry{Purpose: purpose, Success: false, Error: err.Error()})
	rm.fireRotationHooks(purpose, "", "", "manual", false, err)
	return err
}
```

The refusal is a first-class outcome, not a silent no-op:

- it is **audited** with `success=false` and a reason naming the missing wiring,
  so the audit trail tells the truth about what happened;
- it **stores nothing** — no key is generated, promoted or demoted, because a
  key in the registry that nothing reads is the artefact that made the old
  behaviour look real;
- `RotateAll` **propagates the first failure** so a caller cannot mistake a
  refusal for a completed sweep;
- `Start` **starts no scheduler** and logs that rotation is disabled, instead of
  producing a failure every interval.

Two adjacent lies fell out of the same audit. The admin endpoint swallowed the
provider's reload error and always answered `200`; it now returns `500`. And the
CLI command named `rotate-secrets` was performing a reload — so it no longer
dials the daemon at all and reports the refusal locally.

What still works is the operation that actually does something: reloading the
provider chain into the registry, through `SIGHUP` or `POST /reload-secrets`.

## The Principle

A security control has two failure modes. The obvious one is being absent; the
loud one. The dangerous one is being present, plausible, and inert — because
nobody re-examines a control that reports success.

The rule that follows: **when a capability is not wired, refuse at the boundary
in the same shape the working version would use.** Same error type, same audit
field, same exit code. Then the refusal is testable, greppable, and impossible
to mistake for the feature.

This is the same posture the S3 gateway takes towards unimplemented
subresources: an honest `501` is a feature, because it tells the client the
truth instead of pretending to store something.

## How We Verified

- Rotating each of the four purposes returns `ErrRotationNotWired`; the registry
  still reports the original key as `active` with unchanged status.
- Each attempt writes an audit entry with `success=false` and the reason.
- `RotateAll` returns non-nil; `Start` writes no audit entry, proving no timer
  runs.
- A closed audit database does not mask the refusal (the error is still the
  sentinel, not a database failure).
- `/reload-secrets` answers `500` when the provider reload fails, and `200`
  only on success.
- The CLI returns `ENOTSUP` without opening a socket.
- The documentation, the sample configuration and the original architecture post
  were corrected, and the earlier post now opens with a status note.

## What Could Go Wrong

**Operators who relied on the command now get an error.** That is the intent,
but a deployment that scripted `rotate-secrets` in a maintenance job will start
failing. The message names the issue and the alternative (provider reload).

**The registry keeps accumulating versions** from `Reload`, with no consumer.
That is harmless today and becomes the foundation of the real implementation —
but it should not grow a retention policy before it has a reader.

**Refusing is not the end state.** Until rotation is wired, the configured key
lives as long as the deployment does. The refusal makes that visible; it does
not make the key safer.

## When NOT to Use This

If a capability is half-built but its partial behaviour is genuinely useful and
safe, refuse only the parts that lie, not the whole surface. Here, provider
reload is real and stays available; only rotation was refused.

And if you can implement the real thing cheaply, implement it. A refusal is the
right move while the design is unknown, not a substitute for the design.

## References / Dig deeper

- OpenSpec change: `openspec/changes/secrets-rotation-honest-posture`
- Rotation manager: `src/common/rotation.go`
- Admin endpoint: `src/server/metrics_exporter.go`
- The design argument for the real thing: blog post 054, and
  `openspec/changes/r9-secrets`
- Tracking issue: [#1329](https://github.com/alsotoes/momo/issues/1329)
