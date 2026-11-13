---
title: "Making Refusals Visible"
date: "2026-11-10T18:40:00Z"
draft: false
post_type: architecture
tags:
  - security
  - observability
  - audit
categories:
  - metrics
summary: "Audit logs record what succeeded. The requests that matter after an incident are the ones that were refused, and a refusal completes nothing — no success counter moves, nothing is stored. Making denials visible takes a deliberate hook at the refusal point."
artifacts: [{type: spec, path: openspec/changes/denial-observability}, {type: adr, path: docs/adr/0091-denial-observability.md}]
related: ["053-multitenancy-authorization-audit", "070-tenant-quotas", "072-configuration-that-cannot-lie", "073-tenancy-you-can-write-down", "074-when-rotation-does-nothing", "076-write-quorum-that-counts"]
difficulty: "intermediate"
pattern: "denial-observability"
teaches:
  - "Why success-only telemetry hides the attacks you care about"
  - "Why refusals need their own counters instead of an error counter"
  - "How to add observability without breaking existing instrumentation mocks"
---

Every observability layer we had looked at what the system did. The audit log
recorded completed reads, writes and deletes, chained so nobody could edit the
history. The metrics counted uploads, downloads, deletes, replication, bytes and
errors. None of it recorded what the system refused.

That gap matters most in exactly the situation where observability is most needed.
A tenant probing another tenant's bucket gets a `403` on every attempt. The
connection closes, nothing is stored, no operation completes — so nothing appears
in the audit trail, and no counter moves. An operator looking at a dashboard sees
an idle cluster. The same is true of a tenant that has hit its quota: every write
is refused before a single byte is read, so the tenant looks indistinguishable
from one that does not exist.

## Two Different Questions

"Are we receiving traffic?" and "are we rejecting traffic?" are separate
questions, and a single error counter answers neither. A refusal is not an error
in the storage engine — the engine was never reached. It is a decision made before
the operation, which is why it never passed through the instrumentation that sits
after the operation.

So refusals get their own signals:

- `momo_authorization_denials_total` — the request was denied by an ACL check;
- `momo_tenant_quota_refusals_total` — the write would have exceeded the tenant's
  quota.

Two counters rather than one, because the operator's reaction differs. A spike in
authorization denials suggests misconfiguration or probing. A spike in quota
refusals suggests a tenant outgrowing its allocation — a commercial conversation,
not an incident.

Both are declared at zero, which is a small detail with a real consequence: a
dashboard panel that shows nothing is ambiguous, whereas a panel that shows a
flat zero is a statement that the metric exists and nothing has been refused.

## Auditing the Refusal

Counters tell you something is happening; the audit log tells you what. A denial
entry carries the tenant, the operation and the resource, with `outcome=denied`:

```
operation=Authorize resource=object/bucket/docs/a.txt outcome=denied tenant=acme
operation=PutObject resource=too-big.bin       outcome=denied tenant=acme
```

The `Authorize` entry is what turns "someone is being denied" into "acme is being
denied access to this specific key", which is the difference between an alert and
an investigation.

Three properties keep this from becoming a liability. The audit write is
best-effort: a failure is logged and never converts a `403` into a `500`. The
entry is written at the refusal point, before the error response, so it exists
even if the client disconnects immediately. And the counters are incremented
through an *optional* transport interface rather than the existing metrics hook,
so every test double in the repository keeps compiling — the hook type only gains
the methods that daemons actually implement.

## The Refusal You Cannot Afford to Log

Making denials auditable creates a new problem, because a denial is something the
client controls. Every request to a forbidden key now appends a record to the
metadata database, inside a write transaction. A tenant that hammers that key
turns the audit log — the thing that exists to prove nothing was tampered with —
into the resource that fills the disk.

Prefix matching does not save you either, because the resource name is in the
path: an attacker varying the key defeats any scheme that remembers "we already
logged this bucket". Two limits are needed, not one:

- a per-key window, so the same denial is recorded once a minute rather than once
  per request;
- a global rate cap on denial records per second, so distinct keys are bounded too.

The counters stay exact. That is the division of labour: the counter answers "how
many requests are being refused", the audit log answers "who was refused what" with
a sample that is thick enough to investigate and thin enough to survive a flood.
The suppression state itself is a fixed-capacity set, because a limiter that grows
without bound is the same bug wearing a different hat, and the daemon logs one line
per hundred suppressed entries so the sampling is never invisible.

Successful operations are deliberately excluded from the limiter. Nothing a
successful client does is unbounded in the same way, and suppressing the record of
an operation that actually happened would be trading the wrong property.

## What Is Deliberately Not Covered

The native protocols carry no tenant identity and perform no ACL evaluation. They
cannot produce a meaningful denial, and inventing one would be worse than the gap:
an entry attributed to no tenant is noise in a table whose purpose is attribution.
The limitation is documented rather than papered over.

## What It Teaches

Telemetry built around completed operations will always have a hole where the
refusals are, and that hole is widest around the events worth alerting on. When a
decision prevents an operation, the decision itself has to be instrumented — it
does not inherit anything from the path it prevented. And separate counters per
refusal reason are not redundancy: they encode different operator responses to
what looks like the same event.

## References / Dig deeper

- OpenSpec change: `openspec/changes/denial-observability/`
- ADR: `docs/adr/0091-denial-observability.md`
- Tracking issue: [alsotoes/momo#1319](https://github.com/alsotoes/momo/issues/1319)
- Implementation: `src/transport/communicator.go` (`DenialReporter`),
  `src/transport/s3_communicator.go` (refusal paths),
  `src/server/metrics_exporter.go` (counter families)
