---
title: "Governance: AI Review, Spec-First, Three-Dot Diff"
date: 2026-08-24T19:07:36Z
draft: false
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
---
# Governance: AI Review, Spec-First, Three-Dot Diff

Momo's *code* is distributed; its *governance* is too. The project runs on a
steering-rule constitution in `openspec/config.yaml`, enforced by an automated
Gemini reviewer — and the rules governing the rules (Rule 76 blog is one!)
keep evolving.

## The three pillars

1. **AI reviewer** (add-ai-reviewer, 2026-06-09): a `gemini_reviewer.yml`
   workflow reviews every PR against the steering rules — architecture
   patterns, Sentinel/Bolt standards, Rule 73 spec presence, traceability,
   jules-agent protocol.
2. **Spec-First (Rule 73)**: every feature = `proposal.md` + `spec.md`
   (linked issue) + `tasks.md` shipped **with** the code; `Resolves #ID`.
   Bug fixes keep a tracking issue but skip the formal spec.
3. **Three-dot diff gate (#945, steering-three-dot-diff-gate)**: enforcement
   uses `git diff origin/master...HEAD --name-only` — so a Rule 50 master merge
   can't inflate a PR's perceived scope (against both the reviewer *and* the
   benchstat gate). Added anti-rogue-merge + peer checks in the same arc.

## The trust loop

- Reviewer + CI = **gate**; push-comment protocol (Rule 53) keeps the
  conversation visible; a 3-push circuit breaker halts agent loops.
- Spec-first means the "journal" the blog posts describe (this corpus) has a
  stable, auditable ancestor: every change is one PR + one spec + one issue.
- Rule 70 mandates any new steering rule touching review behavior (like **Rule
  76**, this change) be wired into `ai_reviewer.py` in the *same* PR.

## ⚡ Bolt / 🛡 Sentinel of process

The three-dot rule and circuit breakers are "fail-loud, deterministic"
(Sentinel) applied to *process*: tools that silently inflate scope or silently
swallow failures are what governance must catch first. See
[024](024-bolt-performance-engineering.md) for the measurement sibling.

## Related

Benchstat gate: [025](025-benchmark-benchstat-gate.md). Forward roadmap:
[028](028-roadmap-and-research.md).