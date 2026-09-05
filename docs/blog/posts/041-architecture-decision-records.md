---
title: "Architecture Decision Records: Ordering a Growing Documentation Set"
date: 2026-09-02T19:02:00Z
draft: false
tags: [governance, architecture, docs, adr]
categories: [governance]
summary: "Momo adopted Martin Fowler's Architecture Decision Record pattern: each OpenSpec change ships a numbered, status-tracked ADR that records context, decision, consequences, and alternatives — the decision log for a growing codebase."
artifacts:
  - {type: issue, id: "988"}
  - {type: spec, path: openspec/changes/plugin-seam-architecture}
related:
  - 027-governance-ai-review-spec-first
  - 028-roadmap-and-research
  - 030-external-s3-client-replication-downgrade
  - 031-core-integrity-verification
---
As the codebase grew past 30 reference docs, ~40 ratified specs, and 30 blog
posts, one thing was missing: a **decision log**. Why was CRUSH chosen over a
central directory? Why embedded BoltDB over ScyllaDB? Why seams over dynamic
plugins? The answers were scattered across `ARCHITECTURE.md`, `DESIGN_DECISIONS.md`,
and the spec proposals — hard to find, harder to audit, and easy to contradict.

Momo adopted the **Architecture Decision Record** pattern as described by
[Martin Fowler](https://martinfowler.com/bliki/ArchitectureDecisionRecord.html)
(building on [Michael Nygard's original](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions)).

## Why ADRs

Fowler's framing fits the project's trajectory: ADRs are short documents that
capture **a single decision** — the context that motivated it, the decision
itself, its consequences, and the alternatives considered. Their value is
twofold:

1. **A record for later**: months or years from now, anyone can see *why* the
   system is built the way it is.
2. **Clarity in the writing**: forcing trade-offs and alternatives onto paper
   surfaces disagreement before it becomes a codebase contradiction.

They are deliberately **inverted-pyramid** — the decision up front, details
after — and **immutable once accepted**: a changed decision gets a *new* ADR
linked via `Supersedes`, never an edit to the old one.

## How Momo applies it

Momo's specs are the single source of truth (Rule 39) — so the ADR layer does
not re-derive the architecture. Instead, each **ratified OpenSpec change** gets
one ADR in `docs/adr/NNNN-<change-id>.md` that:

- States its **status** (`Accepted` / `Proposed` / `Deprecated`)
- Records the **context** from the spec proposal
- Summarizes the **decision** as requirement summaries
- Lists **consequences** and **alternatives considered**
- Links the spec path, GitHub issue, PR, and blog post (Rule 11/76/78)

39 ADRs now cover the ratified spec set, from `add-cas-storage` (0001) to
`add-e2e-encryption` (0039). Governance is codified:

- **Rule 77** — every feature/enhancement must ship an ADR.
- **Rule 78** — ADRs are synchronized from specs automatically
  (`make adr-sync`), status derives from `tasks.md` completion, confidence is
  computed, and blog links are matched by issue number.
- **Rule 79** — no direct pushes to `master`; doc-only changes are the narrow
  exception, keeping every ADR a reviewed artifact.

## The Fowler contract, honored

| Fowler principle | Momo implementation |
|---|---|
| One decision per record | One ADR per ratified OpenSpec change |
| Inverted pyramid | Status → Context → Decision → Consequences → Alternatives |
| Immutable after acceptance | New ADR with `Supersedes`; old marked `Deprecated` |
| Monotonic numbering | `NNNN-<change-id>.md` |
| Lightweight markdown | `docs/adr/`, validated by CI (`adr-sync-check`) |
| Record of alternatives | Parsed from spec alternatives |

## ⚡ Bolt / 🛡 Sentinel lens

The sync tool is a zero-dependency Go binary (<50ms for 39 specs) — ⚡ **Bolt**
discipline for tooling too. The ADR *contract* is 🛡 **Sentinel** discipline:
every accepted decision is honest about its trade-offs, and a decision cannot
be silently changed — only superseded with a visible link.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets,
and `docs/adr/README.md` for the full process.

## Related

Governance: [027](027-governance-ai-review-spec-first.md). Roadmap:
[028](028-roadmap-and-research.md). Recent ratified decisions: [030](030-external-s3-client-replication-downgrade.md), [031](031-core-integrity-verification.md).
