---
title: "Making the Journal Readable: Explain First, Link Second"
date: 2026-10-02T20:42:44Z
draft: false
post_type: architecture
tags: [governance, documentation, writing]
categories: [governance]
summary: "Our engineering journal had become reference soup — posts you could not understand without opening a spec. We made every post self-contained and pushed repository links into a References section."
artifacts:
  - {type: spec, path: openspec/changes/blog-journal-readability}
  - {type: issue, id: "1116"}
related:
  - 027-governance-ai-review-spec-first
  - 041-architecture-decision-records
---

A reader landed on one of our posts and asked the fairest question you can ask
a technical writer: *"What does this actually do?"*

The post was accurate. Every claim traced to a spec, a pull request, a source
file. It was also unreadable. To understand it you had to open the spec it
pointed at, then the source that spec described, then the code that source
referenced. The post was a signpost, not an explanation — and a signpost is
useless to someone who has not already taken the trip.

## The Real Problem

Our journal had drifted into **reference soup**. The posts were written like
release notes: a list of what shipped, each item tagged with where it lives in
the repository. That style works for a changelog. It fails for a *journal*,
whose whole purpose is to teach the reader why a decision was made and how to
recognize the same problem themselves.

We measured it, and the numbers were blunt. Of 48 posts:

- 46 referenced specification paths;
- 46 referenced internal documentation paths;
- 13 referenced individual source files;
- only about 5 followed the narrative structure our own writing guide asked for.

Worse, seven posts used mathematical notation in a syntax our site never
rendered, so readers saw raw dollar-sign delimiters instead of formulas. The
prose was trying to be precise and ended up looking broken.

The root cause was a single confused idea: **we treated a link as an
explanation.** It is not. A link is a promise that the answer exists somewhere
else. A post has to keep that promise itself.

## Why the Obvious Solutions Failed

**"Add more links."** We already had plenty. Adding a pointer to every claim
makes a post *navigable*, not *understandable*. The reader still has to leave.

**"Add a math renderer."** This fixes the seven broken formulas but leaves the
other forty-one posts untouched. It also adds a client-side dependency to render
expressions that are clearer written out in words — "roughly 64 bytes of state
per active blob" beats a formula nobody can see.

**"Delete the references."** This over-corrects. The pointers are genuinely
useful to a reader who wants depth: the spec, the pull request, the source. The
problem was never that the references existed — it was that they carried the
explanation.

## The Solution: Explain First, Link Second

We adopted one rule and made it enforceable:

> **A post must be understandable on its own.** Explain the concept in the post.
> Repository, spec, and pull-request references belong in a final
> *References / Dig deeper* section, for readers who want to go further.

Concretely, every post now:

1. **Explains before it names.** State the problem and the approach in plain
   language before mentioning an internal file, type, or spec.
2. **Defines jargon on first use.** One sentence a newcomer can follow for terms
   like "splay", "CRUSH", "seam", or "tombstone".
3. **Confines references to a `References / Dig deeper` section.** The narrative
   body stands without them.
4. **Avoids unrendered markup.** Formulas are written as prose or code.

The rule only matters if it survives contact with the next author, so we taught
the validator to check it. The blog gate now fails a post that is missing a
References section, opens as a bare changelog list, cites a repository path in
the narrative body, or contains unrendered math. The rule is no longer a
guideline someone can forget; it is a build condition.

## Principle Callout

> **Pattern: Explain First, Link Second**
> Treat links as optional depth, never as the explanation. Write the post so it
> teaches the idea on its own; put pointers to specs, source, and pull requests
> in a final References section.
>
> **Applies when**: any narrative meant to be read by someone who has not seen
> the codebase — journals, release write-ups, incident postmortems, onboarding
> docs.
> **Doesn't apply**: pure reference material. A specification, an API reference,
> or a changelog is *supposed* to be dense and link-heavy; that is its job.

## How We Verified

The check is mechanical, so verification is too. A synthetic test suite feeds
the validator deliberately broken posts — a missing References section, a
source path in prose, an unrendered formula, a changelog opening — and confirms
each is caught, while a compliant post and a path inside a code block pass. The
validator runs on every pull request that touches the journal, alongside the
existing front-matter and diagram checks.

The measure of success is simple: pick any post, delete every link from its
body, and read it. If it still teaches the idea, it is self-contained.

## Failure Modes

| Risk | Guard |
|------|-------|
| Over-correction: stripping genuinely useful pointers | References section preserves the depth trail |
| False positives on paths inside code examples | The validator strips fenced code blocks before scanning |
| The required mindset link being flagged | The standards link is explicitly allowed anywhere |
| The rule being ignored again | It is enforced in CI, not just documented |

## When NOT to Use This

- **Reference material.** Specs, API docs, and changelogs are dense and
  link-heavy by design; do not narrate them.
- **Internal-only notes.** If the only reader has the repository open, a bare
  pointer is fine.
- **When the explanation genuinely does not fit.** Rare, but if a post cannot
  stand alone, that is a signal the subject deserves a longer piece, not a
  shorter one with more links.

## References / Dig deeper

- The writing standard: [docs/blog/WRITING_GUIDE.md](../../blog/WRITING_GUIDE.md)
- The journal spec: [docs/blog/README.md](../../blog/README.md)
- Tracking issue: [#1116](https://github.com/alsotoes/momo/issues/1116)
- Governance of the journal: [027](027-governance-ai-review-spec-first.md),
  decision records: [041](041-architecture-decision-records.md)
