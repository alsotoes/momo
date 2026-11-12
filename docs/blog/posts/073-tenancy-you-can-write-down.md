---
title: "Tenancy You Can Actually Write Down"
date: "2026-11-10T21:20:00Z"
draft: false
post_type: architecture
tags:
  - multi-tenancy
  - authorization
  - configuration
categories:
  - s3
summary: "The tenancy code read three pieces of state that only tests ever wrote: tenant records, bucket ACLs and quota usage. Every layer was implemented, tested and unreachable. The fix is a bootstrap that makes configuration the source of truth — and a prune rule careful enough that it cannot lock you out."
artifacts: [{type: spec, path: openspec/changes/declarative-tenancy}, {type: adr, path: docs/adr/0093-declarative-tenancy.md}]
related: ["053-multitenancy-authorization-audit", "070-tenant-quotas", "071-making-refusals-visible", "072-configuration-that-cannot-lie", "074-when-rotation-does-nothing", "075-dynamic-replication-order"]
difficulty: "intermediate"
pattern: "declarative-bootstrap"
teaches:
  - "How a fully implemented feature can be unreachable because nothing writes its state"
  - "Why a startup bootstrap beats an admin API for a small deployment"
  - "How to prune declarative state without the risk of wiping everything"
---

Multi-tenancy had been "done" three times. Tenant records existed. The key
hierarchy existed and was tested. ACLs had a tri-state evaluation, a default-deny
policy and per-operation enforcement. Quotas were enforced in a single
transaction, before the payload was read. Audit attribution worked. Refusals were
counted and rate-limited.

None of it could be used.

The reason is a single question: *who writes the tenant record?* The S3 handshake
resolves a token by looking it up in the `tenant_meta` bucket. That bucket is
written by `PutTenantConfig`. And `PutTenantConfig` was called from exactly one
kind of code: tests. A tenant declared in configuration was parsed, validated,
logged about — and then forgotten. Presenting its token got you a `403`, because
the handshake had never heard of it.

ACLs were worse. There was no surface at all: no config key, no CLI command, no
admin route. The types existed, the store methods existed, the evaluator existed,
and the only way to grant a permission was to call a Go function.

## The Shape of This Bug

It is the third instance of the same failure in the last few weeks, and the pattern
is specific enough to name: **a layer reads state that nothing writes**. Not a
stub, not a `TODO` — working code, on a path that no operator can reach. Earlier
instances were a struct field read by three subsystems and assigned by none, and a
configuration key parsed into a value nobody consumed.

The reason these survive review is that every piece looks finished in isolation.
The evaluator has tests. The store has tests. The only untested thing is the seam
between *configuration* and *store*, which is precisely the seam that defines
whether a deployment works.

## Configuration as the Source of Truth

For a deployment of this size, an admin API would be the wrong first answer. It
needs authentication, a wire format, an audit trail, a client, and an operator who
remembers to call it before the first request. Configuration already has all of
that infrastructure: it is authenticated by file permissions, versioned in git,
auditable as a diff, and applied at startup.

So the bootstrap is deliberately small:

1. every `[tenant.<id>]` section is upserted into the tenant store;
2. every `[acl.<bucket>]` section is upserted into the ACL bucket;
3. entries in the store that configuration no longer declares are pruned;
4. all of it runs before the listener opens, and a failure aborts startup.

The fourth point is the one that matters most. If seeding fails halfway and the
daemon starts anyway, the operator gets a healthy-looking process that denies every
tenant — the worst possible outcome, because it looks like an authorization problem
rather than a startup problem.

## The Prune Rule Nobody Gets Right

Pruning is where declarative systems hurt people. If "absent from configuration"
means "delete", then a typo in a section name, or a failed config load that falls
back to defaults, silently revokes every tenant. That is a denial-of-service you
inflicted on yourself during a deploy.

The rule here is narrow: **prune only when the corresponding section declares at
least one entry.** Removing one of three tenants revokes that one on the next
start. Dropping the whole `[tenant.*]` block removes nothing, because an empty
section is indistinguishable from "this daemon does not manage tenancy" — and in
that case doing nothing is the only safe action.

There is an honest limitation: when tenancy is not configured at all, stale records
persist. That is the correct trade for a system whose other option is revocation by
omission.

## ACLs From Text

The ACL section is deliberately terse, because a verbose permission syntax is one
nobody writes correctly:

```ini
[acl.photos]
acme = read,write
beta = read,!delete
*    = read
```

A key is a tenant (`*` for everyone), a value is a permission list, and a leading
`!` marks an explicit deny. That last character is what makes the tri-state
evaluator reachable from configuration: without it every configured entry is an
allow, and `deny` exists only in code. Validation is strict for the same reason the
evaluator exists — a typo like `reed` must fail the load rather than silently grant
nothing (or, worse, be ignored and grant the default).

## What Proves It Works

The test that matters is not a unit test of the parser. It is this: a tenant that
exists only in a configuration struct, a bucket ACL that exists only in that same
struct, a real CAS store, a real S3 handshake over a socket — and the request
reaching dispatch. Then the same tenant on a bucket with no section, getting `403`,
because default-deny must survive the bootstrap rather than be bypassed by it.

Two tests, one seam, and the feature is finally reachable. Everything else was
already there.

## What It Teaches

An implementation is finished when an operator can use it, not when its functions
have tests. The audit that finds "nothing writes this state" is a question you can
ask mechanically — for every read of a persistent structure, ask which code path
writes it, and whether that path is reachable from configuration or an API. And for
declarative state, the prune rule deserves more thought than the seed rule: seeding
too much is annoying, pruning too much is an outage.

## References / Dig deeper

- OpenSpec change: `openspec/changes/declarative-tenancy/`
- ADR: `docs/adr/0093-declarative-tenancy.md`
- Tracking issue: [alsotoes/momo#1325](https://github.com/alsotoes/momo/issues/1325)
- Implementation: `src/server/tenancy_bootstrap.go`, `src/common/config.go`
  (`loadACLConfig`), `src/storage/storage.go` (`ListBucketACLs`, `DeleteBucketACL`)
