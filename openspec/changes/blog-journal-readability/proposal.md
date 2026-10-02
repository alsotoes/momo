# Change: Blog Journal Readability

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1116 (docs(blog): make the engineering journal self-contained and readable)
- https://github.com/alsotoes/momo/issues/964 (blog-posts-hugo: journal foundation)

## Why

The engineering journal (`docs/blog/`, https://momo.apps.headup.ws/) is hard to
read. Most posts are changelog/release-note style: they cite repository
locations (OpenSpec spec paths, `src/` file names, internal identifiers, Rule
numbers) **as the explanation**, so a reader cannot understand a post without
opening a spec or source file.

Measured across the 48 posts:

- 46/48 reference `openspec/` paths; 46/48 reference `docs/` paths; 13/48
  reference `src/` file paths.
- Only ~5/48 follow the `WRITING_GUIDE.md` teachable-narrative structure.
- 7 posts use LaTeX (`$0\text{ bytes}$`) but `hugo.toml` has no math renderer,
  so the expressions render as literal text.

The `blog-posts-hugo` spec currently says posts are a "narrative/link layer"
that should "link source artifacts in the body rather than duplicating docs
content (DRY)". In practice this produced reference soup. The standard needs to
be sharpened: **explain in the post; reference only for depth.**

## What Changes

- **Ratify a readability standard.** Every post MUST be a self-contained,
  teachable narrative: it explains the concept in prose and does not require the
  reader to open a repository file. Repository/spec/PR/doc references are
  confined to a final `References / Dig deeper` section (and the front-matter
  `artifacts`/`related` fields).
- **Define required structure.** Narrative opening (the real problem), why
  obvious solutions failed, the solution with annotated code, a reusable
  principle, verification, failure modes, when NOT to use it, and References.
- **Enforce it in CI.** `.github/scripts/blog_check.py` gains structural checks:
  a narrative opening and a `References`/`Dig deeper` section are required;
  `openspec/`/`src/`/`docs/` paths used in prose outside the References section
  are flagged.
- **Rewrite the corpus.** All 48 posts are rewritten in thematic batches to meet
  the standard.
- **Fix rendering.** Unrendered LaTeX is converted to plain prose/code (no new
  client-side math dependency).

## Non-Goals

- No change to the site's visual design, navigation, search, or deployment.
- No change to front-matter schema (`blog-posts-hugo` remains authoritative for
  `title`/`date`/`draft`/`tags`/`categories`/`summary`/`artifacts`/`related`).
- No change to the `docs/momofs/` implemented-state rule.
- No addition of a math renderer; math is expressed as prose or code.

## Impact

- **Readers:** every post is understandable on its own; references are optional
  depth, not prerequisites.
- **Authors/agents:** a clear, enforced structure (Rule 76) removes ambiguity
  about what a post must contain.
- **CI:** `blog-check` catches readability regressions (missing narrative or
  References section; prose-path references).
- **Specs:** the `blog-posts-hugo` DRY clause is reconciled (links support the
  explanation, they do not replace it).
