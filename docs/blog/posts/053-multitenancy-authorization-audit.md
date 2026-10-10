---
title: "Tenant Isolation: Deriving Per-Tenant Keys, Enforcing ACLs, and Proving What Happened"
date: "2026-08-25T03:25:29Z"
draft: false
post_type: architecture
tags:
  - security
  - multi-tenancy
  - encryption
  - audit
  - authorization
categories:
  - encryption
  - storage
  - s3
summary: "How momo isolates tenants without a central secret server: HKDF-derived per-tenant keys, default-deny ACLs evaluated per request, and a SHA-256 hash-chained audit log that makes tampering detectable. Teaches: key derivation vs. key storage, why ACL evaluation order matters, and how hash chaining turns a log into evidence."
artifacts:
  - type: spec
    path: openspec/changes/r8-multitenancy-authz
related: ["013-e2ee-envelope-encryption", "002-replication-strategies-polymorphic", "044-plugin-seam-architecture", "054-secrets-management-key-rotation"]
difficulty: "advanced"
pattern: "security-boundary"
teaches:
  - "HKDF per-tenant key derivation"
  - "default-deny ACL evaluation"
  - "hash-chained audit logs"
---

A shared object store has a trust problem the moment two organizations put data
in it: organization A must never read, overwrite, or even enumerate
organization B's objects, and when something goes wrong, both parties must be
able to prove which operations actually happened. That is the multi-tenancy
problem, and it is easier to get subtly wrong than it looks. This post explains
how momo isolates tenants, why the obvious shortcuts fail, and what makes its
audit trail trustworthy.

## The Real Problem

Three requirements have to hold at the same time:

1. **Confidentiality and integrity across tenants.** Tenant A's key must not
   open tenant B's data, and compressing or deduplicating identical content
   across tenants must not leak the fact that the content is identical.
2. **An authorization decision for every request.** Reads, writes, deletes,
   and listings all need to be checked, not just the "dangerous" ones.
3. **A trustworthy record.** When a dispute arises, the operator must be able
   to show what happened, and prove that the record was not edited after the
   fact.

Doing all three centrally — one key server, one policy database, one log
server — reintroduces exactly the single point of failure the rest of the
system was built to avoid.

## Why the Obvious Solutions Failed

**One shared encryption key with a tenant column in the metadata.** This is the
most common shortcut. It fails the confidentiality requirement the moment the
metadata database is read by the wrong process, and it makes per-tenant key
rotation impossible without re-encrypting everyone.

**Deriving keys by storing them.** The second shortcut is to generate a random
key per tenant and store it (encrypted) next to the tenant record. Now the
tenant record is a single high-value secret, and every node that mints a tenant
key needs a round trip to fetch it. That is a dependency on a reachable secret
store — a single point of contention, if not failure.

**Checking authorization only on writes.** Reads leak as much as writes.
Listing all objects is a read, and if it is unchecked, a tenant learns the
shape of the whole cluster. A partial policy is a broken policy.

**An audit log that is just an append.** An append-only file is easy to write
and easy to edit. Without a mechanism that binds each entry to the previous
one, an attacker with storage access can delete or rewrite entries and the log
looks perfectly normal.

## The Solution with Annotated Code

### Per-tenant keys by derivation, not storage

Instead of storing a key per tenant, momo derives it from the root key, using
the tenant ID as domain-separation input. The root key is the only secret that
must exist; every tenant key is a pure function of it.

```go
// Each tenant gets its own KEK, OPRF share, and auth token, all derived from
// the root material with the tenant ID as the HKDF "info" (domain separation).
kek    := HKDF(rootKEK,  info="tenant/"+tenantID+"/kek")
share  := HKDF(rootShare, info="tenant/"+tenantID+"/oprf")
```

Because derivation is deterministic, any node can reconstruct a tenant's key
locally from the root material it already holds. There is no tenant-key fetch
step, so there is no tenant-key round trip to fail. The tenant's KEK is then
wrapped under the root key for storage:

```go
// WrapTenantKEK seals the tenant KEK with AES-256-GCM using the tenant ID as
// additional authenticated data, binding the ciphertext to its tenant.
wrapped, _ := WrapTenantKEK(rootKEK, tenantKEK, tenantID)
```

Binding the tenant ID as associated data matters: a wrapped key cannot be
"moved" to another tenant without the unwrap failing, because the associated
data will not match.

### Default-deny ACL evaluation

Authorization is evaluated against explicit entries, and the default when
nothing matches is **deny**. Deny always wins over allow.

```go
func checkACL(entry *ACLEntry, tenantID string, perm Permission) bool {
    if entry.TenantID != tenantID {
        return false          // not this tenant's rule
    }
    if !entry.Permission.Has(perm) {
        return false          // rule does not cover this permission
    }
    return entry.Effect == "allow"
}
```

The order of evaluation is the security-critical part. A request is allowed
only if an explicit `allow` matches; an explicit `deny` short-circuits to
rejection. There is no "no rule, so allow" branch — the absence of a rule is a
denial.

### Making the audit log tamper-evident

Each entry stores the hash of the entry before it. Editing any entry breaks the
chain from that point forward, so verification is a single walk.

```go
// entry.PrevHash is the EntryHash of the previous entry. The first entry
// chains from the empty hash, giving the chain a fixed genesis.
entry.PrevHash  = lastEntryHash
entry.EntryHash = SHA256(canonical(entry))
```

Verification recomputes every hash and checks that each entry's `PrevHash`
equals its predecessor's `EntryHash`. A single altered byte anywhere makes the
chain diverge.

> **Principle:** Derive secrets you can reconstruct; store secrets you cannot.
> Every piece of state a node must fetch to do its job is a piece of state that
> can be unavailable at the worst moment.

## Verification

The design is exercised by three tests that correspond to the three
requirements:

- **Key derivation** proves two tenants never produce the same key, and that
  wrap/unwrap round-trips and rotation preserve decryptability.
- **Authorization** proves an `allow` entry admits the matching tenant while a
  missing entry, a different tenant, or an explicit `deny` rejects.
- **Audit chaining** proves a valid chain verifies, and that mutating an entry
  causes verification to fail.

## Failure Modes

- **Root key compromise defeats tenant isolation.** Derivation is only as strong
  as the root. Rotating the root key re-derives every tenant key, which is the
  intended recovery path.
- **ACL misconfiguration is fail-closed but can lock out a tenant.** A missing
  `allow` denies, which is safe but visible as access denials rather than silent
  access.
- **Audit chaining detects tampering but does not prevent it.** A party with
  write access can truncate the log from the end. Detecting truncation requires
  an external witness (publishing the latest hash periodically).

## When NOT to Use

If every client is fully trusted and there is only one tenant, the per-tenant
key derivation and ACL machinery add overhead and configuration for no security
benefit. Run single-tenant with encryption-at-rest and skip the tenant layer.
The audit log is still worth enabling for its operational value.

## References / Dig deeper

- Multi-tenancy, authorization, and audit specification: `openspec/changes/r8-multitenancy-authz`
- Per-tenant key hierarchy: `src/crypto/crypto.go`
- ACL storage and evaluation: `src/storage/storage.go`
- Audit chaining: `src/storage/storage.go` (`WriteAuditLog`, `VerifyAuditLog`)
- Tracking issue: https://github.com/alsotoes/momo/issues/936
