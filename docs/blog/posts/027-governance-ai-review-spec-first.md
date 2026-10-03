---
title: "Governance: AI Review, Spec-First, Three-Dot Diff"
date: 2026-08-24T19:07:36Z
draft: false
post_type: architecture
tags: [go, governance, ai-review, spec, automation]
categories: [governance]
summary: "How momo governs itself: a Gemini AI reviewer, Rule 73 spec-first, three-dot diff gates, and the automation rules that keep agents honest."
artifacts:
  - {type: pr, id: "909"}
  - {type: pr, id: "945"}
  - {type: spec, path: openspec/changes/add-ai-reviewer}
  - {type: spec, path: openspec/changes/steering-rule-73-spec-first}
  - {type: spec, path: openspec/changes/steering-three-dot-diff-gate}
  - {type: doc, path: docs/AI_FLYING_SOLO.md}
related:
  - 025-benchmark-benchstat-gate
  - 028-roadmap-and-research
  - 041-architecture-decision-records
  - 052-rule-93-duplicate-jules-prs
---

Momo is a distributed storage system, and its *code* is distributed across many
contributors — human and automated. Its *governance* had to be distributed too,
or it would not scale. This post is about the constitution we wrote for
ourselves and the machine that enforces it.

First, the terms. **OpenSpec** is our spec-first change workflow: before code
lands, a change is described in a proposal, a specification of required
behavior, and a task list. **Steering rules** are the numbered constitution of
the project — rules about architecture, security, performance, and process —
kept in one configuration file so that every agent (and human) reads the same
source. A **pull request (PR)** is a proposed set of commits; a **diff** is the
difference it introduces. The **Gemini reviewer** is an automated reviewer that
reads a PR and checks it against those rules.

## The Real Problem

We let AI agents contribute at scale. That is powerful and terrifying: an agent
can open a plausible-looking PR that quietly violates an architectural
invariant, skips the spec, or inflates its scope by merging the main branch into
its feature branch. Review-by-humans alone could not keep up, and inconsistent
review meant the rules drifted. We needed governance that was written down,
machine-checkable, and applied identically to every contribution.

## Why the Obvious Solutions Failed

**"Review harder."** Human attention does not scale, and rules that live only in
reviewers' heads are applied unevenly.

**"Just document the rules."** A rule nobody enforces is a suggestion. We had
seen specs skipped when a change "obviously" did not need one.

**"Trust the agent's own summary."** An agent's description of its change is
exactly the thing that can be wrong or misleading. We needed to inspect the
actual bytes.

## The Solution: Three Pillars

**1. An automated reviewer.** Every PR triggers a review workflow that checks
the change against the steering rules: architecture patterns, the Bolt
(performance) and Sentinel (security) standards, whether a spec exists, and
whether the change traces to a tracked issue. The reviewer is a gate, not a
commenter: a red check blocks the merge.

**2. Spec-first (Rule 73).** Every feature or enhancement ships with its
proposal, its specification, and its task list *in the same PR as the code*,
linked to an issue. Bug fixes keep a tracking issue but are allowed to skip the
formal spec — a fix restores documented behavior, it does not introduce a new
contract. This is what makes our history auditable: one change equals one PR,
one spec, one issue.

**3. The three-dot diff gate.** Here is the subtle one. When an agent merges the
main branch into its feature branch to resolve conflicts, the resulting diff can
appear to touch everything the main branch touched. A two-dot diff (comparing
the branch tip against main) would show all of that noise and make a tiny change
look enormous — to the reviewer *and* to the performance-regression gate. We
switched enforcement to the *three-dot* diff, which compares against the common
ancestor of the branch and main, so only the branch's own changes count. The
same arc added checks for rogue merges and a peer-review requirement.

## The Trust Loop

The reviewer and continuous integration form the gate. A push-comment protocol
keeps the conversation on the PR, and a three-push circuit breaker stops an agent
that is looping — after three failed attempts, it halts instead of burning tokens
and churning the branch. Spec-first gives the journal you are reading a stable
ancestor: the blog post, the spec, the PR, and the issue all point at the same
change. And a rule about rules: any new steering rule that changes review
behavior must be wired into the reviewer in the same PR that adds the rule — no
rule can exist that the machine does not know about.

> **Pattern: Machine-Enforced Governance for Agent Contributions**
> Write process rules once, in a single machine-readable source, and have an
> automated gate enforce them on every change. Keep the rule set and the
> enforcement code in lockstep.
>
> **Applies when**: many contributors (human or AI) share a codebase and
> consistency matters more than per-change judgment.
> **Doesn't apply**: tiny projects where a single maintainer holds the whole
> context; the ceremony would outweigh the benefit.

## ⚡ Bolt / 🛡 Sentinel of process

The three-dot rule and the circuit breaker are "fail loud, deterministic"
applied to *process*. Tools that silently inflate scope, or silently swallow
failures, are exactly what governance must catch first. The same discipline that
keeps the data path honest keeps the development path honest.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Specs: `openspec/changes/add-ai-reviewer`,
  `openspec/changes/steering-rule-73-spec-first`,
  `openspec/changes/steering-three-dot-diff-gate`.
- The rule constitution: `openspec/config.yaml`.
- Agent guide: [docs/AI_FLYING_SOLO.md](../../AI_FLYING_SOLO.md).
- Sibling posts: benchstat gate [025](025-benchmark-benchstat-gate.md),
  forward roadmap [028](028-roadmap-and-research.md),
  decision records [041](041-architecture-decision-records.md).
