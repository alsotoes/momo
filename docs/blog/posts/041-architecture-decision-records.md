---
title: "Architecture Decision Records: Ordering a Growing Documentation Set"
date: 2026-09-02T19:02:00Z
draft: false
post_type: architecture
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

As a codebase grows, the hardest question is rarely "what does this code do?" It
is "why is it built this way?" Months after a decision, the reasoning is gone —
buried in a chat thread, a spec proposal, or a reviewer's memory. We had passed
thirty reference documents, dozens of ratified specs, and thirty blog posts, and
the answers to basic questions had scattered across all of them.

This post is about the fix: an **Architecture Decision Record (ADR)**, a short
document that captures exactly one decision — the context, the choice, its
consequences, and the alternatives that lost. The pattern comes from Martin
Fowler, building on Michael Nygard's original write-up.

## Why ADRs

An ADR is deliberately small and deliberately narrow. Its value is twofold.
First, it is **a record for later**: years from now, anyone can read *why* CRUSH
placement was chosen over a central directory, or why an embedded key/value store
was chosen over a separate database. Second, it is **clarity in the writing**:
forcing trade-offs and alternatives onto the page surfaces disagreement while it
is still cheap — before it hardens into two parts of the codebase contradicting
each other.

Two properties make ADRs trustworthy. They are **inverted-pyramid**: the decision
is stated up front, with the detail below it. And they are **immutable once
accepted**: a decision is never silently edited. If it changes, a *new* record is
written and linked to the old one as its successor, and the old one is marked
deprecated. The history of reasoning is append-only.

## How Momo Applies It

Our specs are the single source of truth for required behavior, so the ADR layer
does not re-derive the architecture. Instead, each ratified change gets one
record that:

- states its **status** (accepted, proposed, or deprecated);
- records the **context** from the proposal;
- summarizes the **decision** as requirement summaries;
- lists **consequences** and **alternatives considered**;
- links back to the spec, the issue, the pull request, and the blog post.

Thirty-nine records now cover the ratified spec set, from the first storage
change to end-to-end encryption. The process itself is codified in three steering
rules: every feature or enhancement must ship a record; records are synchronized
from specs automatically (status derived from task completion, with blog links
matched by issue number); and direct pushes to the main branch are disallowed,
with documentation-only changes the narrow exception — so every record is a
reviewed artifact.

## The Fowler Contract, Honored

| Fowler principle | Momo implementation |
|---|---|
| One decision per record | One record per ratified change |
| Inverted pyramid | Status → Context → Decision → Consequences → Alternatives |
| Immutable after acceptance | New record with a successor link; old one deprecated |
| Monotonic numbering | `NNNN-<change-id>.md` |
| Lightweight markdown | Validated by continuous integration |
| Record of alternatives | Parsed from spec alternatives |

> **Pattern: One Decision, One Immutable Record**
> Record every significant architectural decision as a short, numbered,
> status-tracked document that states context, decision, consequences, and
> alternatives — and never edit it after acceptance; supersede it instead.
>
> **Applies when**: a project accumulates decisions whose rationale must outlive
> the people who made them.
> **Doesn't apply**: reversible, low-stakes choices — a record per variable name
> is bureaucracy, not history.

## ⚡ Bolt / 🛡 Sentinel lens

The sync tool is a zero-dependency Go binary that processes the whole set in
under a minute — ⚡ **Bolt** discipline applied to tooling. The record *contract*
is 🛡 **Sentinel** discipline: every accepted decision is honest about its
trade-offs, and a decision cannot be silently changed — only superseded with a
visible link.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Process and records: `docs/adr/README.md`, `docs/adr/NNNN-<change-id>.md`.
- Spec: `openspec/changes/plugin-seam-architecture`.
- External: [Martin Fowler on ADRs](https://martinfowler.com/bliki/ArchitectureDecisionRecord.html),
  [Michael Nygard's original](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).
- Sibling posts: governance [027](027-governance-ai-review-spec-first.md),
  roadmap [028](028-roadmap-and-research.md),
  recent decisions [030](030-external-s3-client-replication-downgrade.md),
  [031](031-core-integrity-verification.md).
