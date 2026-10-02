> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1116

# blog-journal-readability Specification

## Purpose

Make every engineering-journal post a self-contained, teachable narrative:
readable on its own, with repository/spec/PR references confined to a final
"References / Dig deeper" section. This is a documentation/governance change; it
does not alter runtime, storage, protocol, or transport behavior.

## ADDED Requirements

### Requirement: self-contained narrative

Every post SHALL explain its subject in prose so a reader understands it without
opening any repository file. The post MUST NOT rely on an OpenSpec spec path,
source file path, doc path, internal identifier, or rule number as the
explanation; such references are supporting pointers only.

#### Scenario: concept explained before the implementation name
- **GIVEN** a post about a mechanism (e.g. self-heal rebuild, S3 checksums)
- **WHEN** the mechanism is introduced
- **THEN** the post states the problem and the approach in plain language before
  naming internal files, types, or spec paths

#### Scenario: jargon defined on first use
- **GIVEN** a post that uses a project term (e.g. "Splay", "CRUSH", "OPRF",
  "seam", "tombstone")
- **WHEN** the term first appears
- **THEN** the post defines it in one sentence a newcomer can follow

### Requirement: references confined to a dig-deeper section

Repository, spec, PR, issue, and doc references in the body SHALL be confined to
a final `References` (or `Dig deeper`) section. The narrative body SHALL NOT
contain bare repository paths (`openspec/…`, `src/…`, `docs/…`, `.github/…`) or
source-file names as load-bearing explanation.

#### Scenario: path used inside the narrative
- **GIVEN** a post whose body contains `openspec/changes/x/` or
  `src/transport/y.go` outside the References section
- **WHEN** the blog validator runs
- **THEN** it reports a violation for that post

#### Scenario: references section present
- **GIVEN** any post
- **WHEN** the blog validator runs
- **THEN** the post has a `References` or `Dig deeper` heading

### Requirement: required narrative structure

Each post SHALL contain a narrative opening that states the real problem, and a
`References`/`Dig deeper` section. Posts SHOULD additionally cover: why obvious
solutions failed, the solution with annotated code, a reusable principle,
verification/measurement, failure modes, and when NOT to apply the pattern
(per `docs/blog/WRITING_GUIDE.md`).

#### Scenario: missing narrative opening
- **GIVEN** a post that opens with a changelog list and no problem statement
- **WHEN** the blog validator runs
- **THEN** it reports the missing narrative opening

### Requirement: rendering correctness

Posts SHALL NOT contain unrendered markup. In particular, LaTeX math (`$…$`)
SHALL NOT be used while the site has no math renderer; expressions MUST be
written as prose or code.

#### Scenario: LaTeX in a post
- **GIVEN** a post containing `$0\text{ bytes}$`
- **WHEN** the blog validator runs
- **THEN** it reports the unrendered math

## MODIFIED Requirements

### Requirement: artifacts and cross-links (blog-posts-hugo)

Posts SHALL link source artifacts in a `References`/`Dig deeper` section rather
than embedding them as the explanation in the body. Front-matter `artifacts`
(openspec change paths, PR IDs, issue IDs) and `related` (sibling post filenames)
remain the canonical cross-link surface; body references support the explanation,
they do not replace it.

#### Scenario: source artifact referenced
- **GIVEN** a post that needs to point at its spec or PR
- **WHEN** authored
- **THEN** the pointer lives in the `References`/`Dig deeper` section (and/or
  front matter), not woven through the narrative

## UNCHANGED Behavior

- Front-matter schema (`title`, `date`, `draft`, `tags`, `categories`, `summary`,
  `artifacts`, `related`) is unchanged; `blog-posts-hugo` remains authoritative.
- Post date = anchor issue/PR `createdAt`; implemented-state grounding against
  `src/`; Bolt/Sentinel tagging and `docs/STANDARDS.md` linkage are unchanged.
- No change to site design, navigation, search, or Cloudflare deployment.
- No math renderer is added.
