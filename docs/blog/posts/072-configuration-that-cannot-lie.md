---
title: "Configuration That Cannot Lie"
date: "2026-11-10T20:10:00Z"
draft: false
post_type: architecture
tags:
  - configuration
  - operations
  - durability
categories:
  - durability
summary: "Four configuration keys were parsed, validated, documented and ignored. One of them promised durability the daemon never delivered. A setting nobody reads is worse than no setting, so the fix is either a wire or a refusal — never silence."
artifacts: [{type: spec, path: openspec/changes/honest-config-surface}, {type: adr, path: docs/adr/0096-honest-config-surface.md}]
related: ["070-tenant-quotas", "071-making-refusals-visible", "064-failing-over-coordination-in-seconds", "073-tenancy-you-can-write-down"]
difficulty: "intermediate"
pattern: "fail-loud-configuration"
teaches:
  - "How to audit a config surface for keys that do nothing"
  - "Why a durability knob that is ignored is a correctness bug, not a cosmetic one"
  - "When to wire a setting and when to refuse it at load time"
---

The pattern keeps repeating in this codebase, which is why it is now a rule: a
configuration key that is parsed, validated, documented and never read. Audit
retention had it. Tenant identity had it. Tenant quotas had it. So we stopped
fixing them one at a time and audited the whole surface instead.

The method is mechanical. Collect every field of every configuration struct, then
search the codebase for a *read* of that field outside the loader — not an
assignment, an actual use. Ninety-eight fields, five hits, and one of them was a
false positive that the compiler would have caught anyway.

## What the Audit Found

Three keys were fully inert:

- `write_quorum` — documented as "a write that cannot reach this many durable
  replicas fails instead of silently acknowledging";
- `metadata_ttl` — documented as the lifetime of a metadata cache that does not
  exist;
- `lease_timeout` — documented as the lease duration, while every caller passes
  its own and the delete path hardcodes ten seconds.

A fourth, `metadata_quorum`, was inert too, but in the most misleading way
possible: the code *did* compute a quorum, hardcoded as a strict majority. An
operator setting two-of-three got three-of-three and would have blamed the
network.

## The One That Mattered

`write_quorum` is the interesting one, because its documentation describes a
durability property. A write is durable when enough independent copies have
acknowledged it. On the blob path the daemon stores locally and forwards to
replicas concurrently — it does not collect their acknowledgements at all, so it
cannot know how many copies exist when it answers the client. Setting
`write_quorum = 2` therefore did not weaken durability; it never strengthened it.
The daemon answered with exactly the same confidence either way.

That is worse than a missing feature. A missing feature is visible: the key does
not exist, the operator asks why. A key that exists and lies is discovered during
an incident, when two nodes are lost and the data is not there.

## Wire, Refuse, or Delete

Three honest outcomes for an inert key, and the right one depends on how far the
implementation is:

1. **Wire it** when the mechanism already exists and only the plumbing is
   missing. `metadata_quorum` was that case: the quorum is a number in a fan-out
   loop, so it became a provider field, clamped to the replica count, with zero
   meaning "strict majority". Both the write and the delete path now use it.
2. **Refuse it** when honouring it needs real work. An explicit `write_quorum`
   above one, or an explicit `metadata_ttl` or `lease_timeout`, now fails at load
   with an error that names the key, says it is not implemented, and links the
   tracking issue. The same reasoning as refusing an S3 feature we cannot
   honestly serve: a clean refusal is a correct answer, silence is not.
3. **Delete it** when nothing can ever honour it. The `MetadataRPCTTL` constant
   documented a cache TTL for a cache that does not exist; it is gone.

The subtlety in the refusal is that it checks *presence*, not value. Writing the
documented default explicitly still fails, because the operator has expressed an
intent the daemon cannot satisfy — and the default value is only a fallback for
nobody-said-anything.

## Two Things This Cost

The first: refusing keys is a breaking change for anyone who set them, so it has
to be loud in the release notes and gentle in the message — which is why the error
names the key, the issue, and says "not implemented yet" rather than "invalid".

The second, and the more embarrassing: the first implementation read the key with
`section.Key("x")` and then checked whether it was present with `section.GetKey`.
The ini library *creates* the key on first access, so every configuration — with
or without the setting — was immediately "present" and refused. Four existing
tests caught it, which is the argument for testing defaults as well as values: a
validation that rejects everything passes every negative test you write.

## What It Teaches

Configuration is an interface, and it lies exactly as much as code does. The audit
that finds inert keys is cheap — a field list and a grep for reads — and it should
run whenever a settings section grows, because the failure mode is invisible by
construction: nothing logs, nothing errors, and the only symptom is a guarantee
that was never true.

## References / Dig deeper

- OpenSpec change: `openspec/changes/honest-config-surface/`
- ADR: `docs/adr/0096-honest-config-surface.md`
- Tracking issue: [alsotoes/momo#1323](https://github.com/alsotoes/momo/issues/1323)
- Implementation: `src/common/config.go` (refusals),
  `src/p2p/metadata_rpc.go` (`SetWriteQuorum`), `src/server/version_replication.go`
