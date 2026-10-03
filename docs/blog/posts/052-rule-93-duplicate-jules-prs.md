---
title: "🧹 One Task, Many PRs: Consolidating Duplicate Jules Branches"
date: 2026-10-03T03:15:53Z
draft: false
post_type: issue
tags: [governance, automation, bolt, reviewer]
categories: [governance]
summary: "Jules opened two PRs for the same optimization. Merging one left the twin open with its own tracker and branch. Rule 93 makes the cleanup a defined step: keep the canonical PR, close the duplicate, close its auto-trace issue, prune the branch."
artifacts:
  - {type: spec, path: openspec/changes/steering-rule-93-duplicate-jules-prs}
  - {type: issue, id: "1121"}
related:
  - 024-bolt-performance-engineering
  - 027-governance-ai-review-spec-first
  - 046-auto-trace-dedup
  - 051-bolt-aws-chunked-zero-alloc
---
A **pull request** is a proposal to change the code: a branch plus a
description. The natural assumption is that one task produces one PR. Our
automated Bolt agent broke that assumption. It opened **two**.

Both PRs implemented the *same* change — a zero-allocation rewrite of one
parser — under slightly different titles. Each carried its own auto-trace
tracking issue. Each had its own branch. We merged one; the other stayed open,
still labeled as work in progress for a task that was already done.

## The problem

When an autonomous agent runs the same task twice, it does not know the runs
converged. **Jules** runs as a background worker: given a task, it branches,
edits, and opens a PR. Two runs of the same task produce two branches and two
PRs that do the same thing — in our case:

- **#1112** "⚡ Bolt: Zero-allocation string parsing for AWS chunked headers"
  (merged),
- **#1114** "⚡ Bolt: Eliminate heap allocations in parseAWSChunkHeader"
  (the twin).

The cost of leaving the twin is real. The tracker shows two open efforts for one
task; a reader cannot tell which is canonical. CI runs twice. And when we merged
#1112, #1114 did not disappear — it kept its branch and its own auto-trace issue
(#1115), because nothing told it to stop. The branch even survived locally as a
stale remote ref, which had to be pruned by hand.

We already had rules for two nearby situations, and neither fit:

- **One PR that needs a clean rebuild** is covered by the Jules takeover rule.
- **The reviewer creating duplicate *issues* for one PR** is covered by the
  auto-trace dedup rule.

Neither says what to do when there are **multiple PRs for the same task**. That
gap is exactly the one that bit us.

## The fix: a defined consolidation step

We added a governance rule — **duplicate Jules PR/branch consolidation** — that
turns "clean up the twin" from an afterthought into a checklist:

1. **Keep one canonical PR.** It is the one that reaches merge, or the one that
   carries the tracking issue.
2. **Close the duplicate** with a comment pointing at the canonical PR (or its
   merge commit): `gh pr close <dup> --delete-branch`.
3. **Close the duplicate's auto-trace issue as superseded**, pointing at the
   canonical tracker — one canonical issue per task, consistent with the
   existing dedup rules.
4. **Prune the branch** (`git fetch --prune`), then confirm
   `gh pr list --state open` shows nothing left for the task.

Written as a rule, the sequence is mechanical. Nothing about it is clever; the
value is that it is *stated*, so the next agent (or human) does not rediscover
it.

> **Principle — make cleanup a step, not a memory.**
> Ad-hoc tidy-up happens when someone remembers. A rule makes the final state
> deterministic: after a merge, the tracker and the branches reflect reality —
> one task, one PR, one tracker.

## How we verified it

Governance changes are verified by parsing and counting, not by tests:

- The steering-rules source of truth now parses and contains rules 1 through 93
  (the new rule is #93), satisfying the single-source-of-truth requirement.
- The rule appears in the agent workflow guide with a matching pitfall entry.
- The three related rules were checked to still read coherently together: one
  PR rebuild, reviewer issue dedup, and now multi-PR consolidation.

## Failure modes

| Risk | Guard |
|---|---|
| Closing the wrong PR (the canonical one) | Canonical is defined as the merged one or the one holding the tracking issue |
| Losing the duplicate's tracker | Its issue is closed **as superseded**, linking the canonical — not deleted |
| The duplicate's branch lingering | Explicit `--delete-branch` + `git fetch --prune` step |
| A third twin appearing later | The post-merge check is `gh pr list --state open` — anything related gets consolidated |

## When NOT to use this

When the "duplicate" is not one. Two PRs that touch the same file are not
necessarily duplicates — they may be independent, conflicting, or exploratory.
Consolidate only when the change is the **same**; otherwise you would close real
work. When in doubt, compare the diffs.

## References / Dig deeper

- The standard: [docs/STANDARDS.md](../../STANDARDS.md)
- The steering rules: `openspec/config.yaml` (Rule 93).
- Related governance posts: [027](027-governance-ai-review-spec-first.md)
  spec-first review, [046](046-auto-trace-dedup.md) reviewer-issue dedup.
- The duplicate pair in question: [051](051-bolt-aws-chunked-zero-alloc.md) the
  merged optimization.
- OpenSpec change: `openspec/changes/steering-rule-93-duplicate-jules-prs/`.
- Tracking issue: [#1121](https://github.com/alsotoes/momo/issues/1121).
