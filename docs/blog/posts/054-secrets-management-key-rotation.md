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
summary: "How momo manages encryption keys without a central secret server: a pluggable provider chain (env, then config file), a per-node versioned key registry in local storage, and rotation that promotes a new key without any downtime. Teaches: why a provider chain beats a single source, how key versioning enables online rotation, and when rotation is and is not worth the complexity."
artifacts:
  - type: spec
    path: openspec/changes/r9-secrets
related: ["053-multitenancy-authorization-audit", "013-e2ee-envelope-encryption", "044-plugin-seam-architecture"]
difficulty: "advanced"
pattern: "seam-over-plugins"
teaches:
  - "provider-chain fallback"
  - "versioned key registry"
  - "online key rotation with grace periods"
---

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

### Rotation as a metadata change

Rotation generates new material, stores it as a new version, and promotes it.
No data is re-encrypted. Dependent components are told to re-read the registry
via a reload signal (an HTTP endpoint and a `SIGHUP` handler).

```go
// The old key stays retrievable; new writes use the new active key. The grace
// period is the retention window during which both are valid.
rm.RotateAll(ctx)   // rotates every purpose: encryption, auth, e2ee, oprf
```

> **Principle:** A secret you can derive or reconstruct is worth more than a
> secret you must fetch. Put the derivable material on every node and keep the
> single source of truth out of the request path.

## Verification

- **Provider chain** proves a miss in the first provider falls through to the
  next, and that an all-miss returns "not found" rather than an error.
- **Rotation audit** proves every rotation writes a chained audit entry with
  the old and new key IDs.
- **Old-key retention** proves that after rotation the previous key is still
  retrievable (for decryption) while the active key has changed.

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
