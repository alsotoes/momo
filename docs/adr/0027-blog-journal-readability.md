# 0027-blog-journal-readability

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- self-contained narrative: Every post SHALL explain its subject in prose so a reader understands it without opening any repository file. The post MUST NOT rely on an OpenSpec spec path, source file path, doc path, internal identifier, or rule number as the explanation; such references are supporting pointers only.
- references confined to a dig-deeper section: Repository, spec, PR, issue, and doc references in the body SHALL be confined to a final `References` (or `Dig deeper`) section. The narrative body SHALL NOT contain bare repository paths (`openspec/…`, `src/…`, `docs/…`, `.github/…`) or source-file names as load-bearing explanation.
- required narrative structure: Each post SHALL contain a narrative opening that states the real problem, and a `References`/`Dig deeper` section. Posts SHOULD additionally cover: why obvious solutions failed, the solution with annotated code, a reusable principle, verification/measurement, failure modes, and when NOT to apply the pattern (per `docs/blog/WRITING_GUIDE.md`).
- rendering correctness: Posts SHALL NOT contain unrendered markup. In particular, LaTeX math (`$…$`) SHALL NOT be used while the site has no math renderer; expressions MUST be written as prose or code. ## MODIFIED Requirements
- artifacts and cross-links (blog-posts-hugo): Posts SHALL link source artifacts in a `References`/`Dig deeper` section rather than embedding them as the explanation in the body. Front-matter `artifacts` (openspec change paths, PR IDs, issue IDs) and `related` (sibling post filenames) remain the canonical cross-link surface; body references support the explanation, they do not replace it. ## UNCHANGED Behavior - Front-matter schema (`title`, `date`, `draft`, `tags`, `categories`, `summary`, `artifacts`, `related`) is unchanged; `blog-posts-h...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1116
- PR: 
- Spec: `openspec/changes/blog-journal-readability/`
- Blog: 

