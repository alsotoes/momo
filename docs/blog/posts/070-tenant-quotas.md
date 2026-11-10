---
title: "Tenant Quotas That Actually Stop a Tenant"
date: "2026-11-10T17:50:00Z"
draft: false
post_type: architecture
tags:
  - multi-tenancy
  - quotas
  - storage
categories:
  - storage
summary: "A quota setting nobody reads is a promise with no mechanism behind it. Bounding a tenant means deciding what to count, refusing the write before its bytes land, and releasing the charge when the write turns out to be invalid."
artifacts: [{type: spec, path: openspec/changes/tenant-quota-enforcement}, {type: adr, path: docs/adr/0097-tenant-quota-enforcement.md}]
related: ["053-multitenancy-authorization-audit", "069-an-audit-log-that-cannot-grow-forever", "071-making-refusals-visible"]
difficulty: "intermediate"
pattern: "resource-quotas"
teaches:
  - "Why a quota must be checked before the payload is written"
  - "Why accounting has to count logical objects, not deduplicated blobs"
  - "Why a tenant credential must bind the tenant or every quota is dead code"
---

The tenant configuration has carried two numbers since multi-tenancy arrived:
a byte limit and an object limit. Both were parsed, validated, documented and
stored. Neither was ever read. An operator who set a ten-gigabyte quota on a
tenant got exactly the same behaviour as an operator who set nothing at all,
which is to say the tenant could fill the node.

This is the same failure we wrote about for audit retention: a configuration key
that is validated but never consulted is worse than a missing key, because it
advertises a guarantee the system does not have. The lesson repeats often enough
to be a rule now — validate the setting, then go find the code that reads it.

## What Has to Be Counted

The obvious answer — count bytes per tenant — hides a decision. The store is
content-addressed: identical bytes are stored once and shared by every name that
references them. If quotas counted physical storage, a tenant storing one hundred
copies of the same file would be charged once, and the quota would measure
something the tenant does not experience. Tenants think in objects. So the
accounting counts logical objects per tenant: bytes are the size of the object
the tenant asked to store, and the object count is the number of names it owns,
whether or not the bytes deduplicate. A tenant can therefore be refused for
storing a thousand aliases of the same blob, which is the correct behaviour for
a quota that exists to bound a tenant's footprint as an API consumer.

## The Check Has to Come First

The shape of an upload makes ordering matter. The gateways parse a request, then
hand the connection to the shared ingest path, which streams the body into the
store while hashing it. If the quota check happened after the store call, a
tenant that is already at its limit would still get its bytes written — then be
told the write failed, and the bytes would have to be deleted. That is not just
wasteful; under a burst it is a disk-exhaustion path that the quota was supposed
to close.

So the ingest path reserves before it reads anything:

1. read the metadata of the existing object under that name, to learn its size;
2. compute the delta — the new size minus the old, plus one object if the name
   is new;
3. apply the delta in a single transaction that also checks the limit;
4. if that fails, refuse with the quota error and never open the payload.

The single transaction is what makes the boundary safe. Two uploads that each fit
individually but not together must not both succeed because they checked
concurrently. A burst of twenty-four writers against a hundred-byte quota with
ten-byte objects admits exactly ten, and a test asserts that number rather than
asserting "roughly".

Step 1 also gives overwrite semantics for free. Replacing an object with a larger
one charges the difference; replacing it with a smaller one frees bytes; and the
object count does not move at all, because the tenant still owns one name.

## Releasing Is Part of the Feature

Every reservation needs a matching release on every path that does not end in a
stored object. There are three: the store call itself failing, a content hash
that does not match the advertised value, and an integrity checksum mismatch. In
all three the object is deleted, so the charge must go with it. A quota
implementation that only accounts on success does not merely over-report usage —
it permanently leaks capacity, and a tenant that retries a failing upload enough
times locks itself out of space it is not using.

Deletes need the mirror treatment, which means reading the object's size before
removing it and charging the bytes and count back afterwards.

## The Bug Behind the Feature

While wiring quotas we discovered that the field the enforcement reads was never
assigned. The S3 gateway has a `tenantID` member; ACL checks, audit attribution
and quota accounting all consult it; nothing set it. Worse, the handshake
rejected the tenant's credential outright, because the only accepted values were
the cluster token and the derived peer token. A tenant declared in configuration
could not authenticate at all.

That failure had been invisible for a release because everything downstream of
the missing field is conditional: no tenant means no ACL evaluation, means no
tenant-attributed audit entry, means quotas that never fire. Each layer was
individually tested by setting the field directly, which is exactly the gap a
unit test cannot see. The fix resolves the presented credential against the
tenant table during the handshake, so the chain becomes live end to end.

Two side lessons came out of the same code. A credential longer than the token
width used to reach a padding helper that panics — recovered, so the server
survived, but the client received no response at all. Attacker-controlled input
reaching a panic path needs a length check before the comparison, not a recover
after it. And a newly declared tenant with no ACL is denied everything, because
ACLs are default-deny: documented, but the first thing an operator hits.

## What It Teaches

Three things generalise beyond this feature. A quota is only real if the check
precedes the work. An accounting rule has to count the unit the caller cares
about, not the unit the storage engine happens to use. And a capability that
several layers read from one field is only as implemented as that field's
assignment — the layers below it can all be correct and still be dead code.

## References / Dig deeper

- OpenSpec change: `openspec/changes/tenant-quota-enforcement/`
- ADR: `docs/adr/0097-tenant-quota-enforcement.md`
- Tracking issues: [alsotoes/momo#1315](https://github.com/alsotoes/momo/issues/1315),
  [alsotoes/momo#1316](https://github.com/alsotoes/momo/issues/1316)
- Implementation: `src/storage/quota.go`, `src/server/file.go` (reserve/release),
  `src/transport/s3_communicator.go` (credential resolution, `403 QuotaExceeded`)
