---
title: "Rotating Secrets Without a Secret Server: Deriving, Versioning, and Hot-Reloading Keys"
date: "2026-08-25T03:25:35Z"
draft: false
post_type: architecture
tags:
  - encryption
  - key-rotation
  - secrets
  - operations
  - security
categories:
  - encryption
  - storage
summary: "How momo sources encryption keys without a central secret server: a pluggable provider chain (env, then config file) and a per-node versioned key registry in local storage — and why rotation is deliberately refused until it can actually change the key in use. Teaches: why a provider chain beats a single source, what key versioning buys you, and why a rotation that reports success without changing anything is worse than no rotation."
artifacts:
  - type: spec
    path: openspec/changes/r9-secrets
related: ["053-multitenancy-authorization-audit", "013-e2ee-envelope-encryption", "044-plugin-seam-architecture", "059-r10-notification", "074-when-rotation-does-nothing"]
difficulty: "advanced"
pattern: "seam-over-plugins"
teaches:
  - "provider-chain fallback"
  - "versioned key registry"
  - "why a no-op rotation is worse than no rotation"
---

> **Status correction (2026-11-11).** This post originally described rotation as
> delivered. It is not: the key registry versions keys, but **no data path reads
> them**, so the daemon keeps the secret it loaded at startup. Rotation now
> refuses with `ENOTSUP` and audits the refusal instead of reporting a success
> that changes nothing (#1329). The explanation below is kept as the design
> argument for what rotation must do before it is wired; the "Rotation as a
> metadata change" section describes the intended behaviour, not the shipped one.

Every system that encrypts data eventually faces the same operational question:
where does the encryption key live, and how do you change it without taking the
service down? The naive answers — a hard-coded key, or a single central vault
every node must reach — either cannot rotate or reintroduce the single point of
failure the rest of the system was built to avoid. This post explains how momo
resolves that question.

## The Real Problem

Three operational requirements collide:

1. **The key must be reachable by every node, always.** If node B cannot reach
   the key store, it cannot serve encrypted data — even though the data is
   right there on its disk.
2. **The key must be rotatable without downtime.** Rotation is a security
   hygiene task: a key may be rotated because of policy, or because it is
   suspected compromised. Either way, in-flight and at-rest data must keep
   working through the change.
3. **The key must not become the new single point of failure.** A central
   secret server is the obvious place to put the key, and the obvious thing to
   go down at 3 a.m.

## Why the Obvious Solutions Failed

**A key in the config file, forever.** This is the simplest option and it works
— until you need to rotate. There is no versioning, so the moment you change
the key, every object encrypted under the old key becomes unreadable. Rotation
is impossible without a migration that re-encrypts everything.

**A single central vault.** This solves "where is the key" but not "what if the
vault is down". Every read now depends on a network round trip to one service.
That is a single point of failure by construction, and it turns a local read
into a distributed one.

**Rotate by re-encrypting everything.** Re-encrypting the whole dataset on every
rotation is correct but ruinous at scale, and it needs a maintenance window. The
right design makes rotation a metadata change, not a data change.

## The Solution with Annotated Code

### A provider chain instead of a single source

Secret lookup is a *chain* of providers tried in order. The common case (a key
in the local config file, or an environment variable injected by the
orchestrator) is answered locally; a remote provider can be added later without
changing any caller.

```go
// BuildProviderChain tries providers left to right and returns the first hit.
// The default chain is "env, momo" (environment, then the config file).
chain, _ := BuildProviderChain(SecretsConfig{Sources: []SecretsSource{SecretsSourceEnv, SecretsSourceFile}})
```

The chain is a seam, not a plugin system: each provider is a compile-time
implementation of one small interface, so there is no dynamic loading and no
new attack surface in the data path.

### A per-node versioned key registry

Rather than a single key, each node keeps a *registry* of key versions in its
own local storage. A key has a status — `active`, `pending_rotation`,
`retired`, or `compromised` — and an identifier. Encrypted data records which
key version it used, so decryption is always possible as long as the version is
retained.

```go
// Rotation stores the new version, then activates it. The previous active key
// is demoted to retired — but NOT deleted, so old ciphertext still decrypts.
newID, _ := registry.StoreKey(ctx, "encryption", material, "AES-256-GCM", "", KeyStatusPendingRotation, 0)
registry.SetActive(ctx, newID)
```

Because the registry lives on each node, any node can resolve the active key
locally. There is no fetch step to fail, and no central server to restate the
whole system's vulnerability.

### Rotation as a metadata change (not yet wired)

The design is that rotation generates new material, stores it as a new version,
and promotes it, with no data re-encrypted and a reload signal telling dependent
components to re-read the registry. That is not what ships today: storing a new
version changes nothing while no data path consults the registry, and re-encrypting
at-rest data needs envelope re-wrapping rather than a metadata swap.

```go
// Refused on purpose: a key nothing reads must not be reported as a success.
err := rm.RotateAll(ctx)   // ErrRotationNotWired (ENOTSUP), audited as a failure
```

What does work is reloading the provider chain into the registry: `SIGHUP` or
`POST /reload-secrets` re-reads env/file, and a provider failure is returned as
`500` instead of an unconditional `200`.

> **Principle:** A secret you can derive or reconstruct is worth more than a
> secret you must fetch. Put the derivable material on every node and keep the
> single source of truth out of the request path.

## Verification

- **Provider chain** proves a miss in the first provider falls through to the
  next, and that an all-miss returns "not found" rather than an error.
- **Refusal is audited** proves a rotation attempt writes a chained audit entry
  with `success=false` and the reason, and stores no key.
- **The registry is untouched** proves a refused rotation neither demotes the
  configured key nor changes its status — the regression guard for the lie this
  correction removes.

## Failure Modes

- **A provider that is configured but unreachable** fails the chain: if the
  first provider errors rather than reporting "not found", the lookup stops.
  Order the chain so the always-local source is first.
- **Retaining retired keys forever** preserves decryptability but enlarges the
  key registry. A retention policy must eventually drop keys whose ciphertext
  is gone — which means those objects can never be decrypted again.
- **Rotation without reload** leaves running components using the old key until
  restarted. The reload signal exists precisely to avoid that.

## When NOT to Use

If the dataset is small and a maintenance window is acceptable, rotating by
re-encrypting under one new key is simpler to reason about than a versioned
registry. And if a managed KMS is a hard requirement, this design is the wrong
shape — it deliberately trades external-KMS integration for zero-SPOF local
resolution.

## References / Dig deeper

- Secrets management and key rotation specification: `openspec/changes/r9-secrets`
- Provider chain and providers: `src/common/secrets.go`
- Versioned key registry: `src/common/keyregistry.go`
- Rotation manager and audit: `src/common/rotation.go`
- Tracking issue: https://github.com/alsotoes/momo/issues/937
- Related: R10 Phase 3 (Notification, [055](059-r10-notification.md))
