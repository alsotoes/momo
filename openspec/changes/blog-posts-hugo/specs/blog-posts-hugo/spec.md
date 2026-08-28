> GitHub Issue URL: https://github.com/alsotoes/momo/issues/964

# blog-posts-hugo Specification

## Purpose

Establish a perpetual narrative layer for the project: Hugo-format journal
posts in `docs/blog/` that record the journey (research, architecture
decisions, engineering tradeoffs, and changes) as features ship, embedded with
the ⚡ Bolt performance and 🛡 Sentinel security mindsets, cross-linked into a
navigable corpus. This is a documentation/governance change; it does not alter
runtime, storage, protocol, or transport behavior.

## ADDED Requirements

### Requirement: steering rule 76 — post-per-ratified-change

The system (all AI agents) SHALL maintain posts under `docs/blog/posts/` in
Hugo static-site format (markdown with YAML front matter), and the governing
rules in `openspec/config.yaml` SHALL include Rule 76 requiring feature and
enhancement PRs to ship a matching blog post in the same PR or an
immediately-following PR.

#### Scenario: feature PR ships with a blog post
- **GIVEN** a feature/enhancement PR
- **WHEN** it is opened
- **THEN** it includes (or is immediately followed by) a `docs/blog/posts/*.md`
  post describing the change, its research, and its architecture decision

#### Scenario: no-blog justification for internal-only change
- **GIVEN** a change with no narrative value (pure internal refactor, no
  behavioral surface)
- **WHEN** no post is authored
- **THEN** the PR body carries an explicit `no-blog` justification, exempting it

#### Scenario: reviewer enforcement
- **GIVEN** an enhancement PR that changes a ratified spec surface and ships
  no `docs/blog/posts/*.md` and no `no-blog` justification
- **WHEN** the AI reviewer inspects it
- **THEN** it reports a Rule 76 violation and withholds approval/merge

### Requirement: implemented-state grounding

Posts MUST describe implemented behavior. Content MUST be verified against
`src/` code and documents under `docs/` outside `docs/momofs/`. The
`docs/momofs/` directory is a design/plan suite and SHALL NOT be cited as
shipped unless the matching implementation exists in `src/momofs`
(`MOUNT_USER_GUIDE.md` and `IMPLEMENTATION.md` §2.3 are the operational
exceptions, ratified by `src/momofs`).

#### Scenario: post cites a docs/momofs design surface without code
- **GIVEN** a post that presents a `docs/momofs/` design feature as implemented
- **WHEN** no matching `src/` code exists
- **THEN** the post violates the implemented-state rule and MUST be revised to
  tag the item as planned/aspirational with no implemented claim

#### Scenario: verification via three-dot diff
- **GIVEN** a PR that adds posts
- **WHEN** the reviewer validates
- **THEN** it checks the posts against real merged code in `origin/master`
  (three-dot diff basis) so a Rule 50 master merge does not inflate scope

### Requirement: post date from issue/PR creation

Each post SHALL carry a `date` front-matter value equal to the `createdAt`
timestamp of its anchor GitHub issue or pull request, queried via
`gh issue view <N> --json createdAt` / `gh pr view <N> --json createdAt`.
Features without an issue anchor SHALL use the earliest code-implementation or
plan commit date derived from git history. Posts SHALL NOT be future-dated.

#### Scenario: anchored feature
- **GIVEN** a post for a feature tracked by a GitHub issue or PR
- **WHEN** authored
- **THEN** `date` equals the artifact's `createdAt` (UTC, RFC3339)

#### Scenario: unanchored early work
- **GIVEN** a post for pre-issue-era work (e.g. genesis, transport evolution)
- **WHEN** authored
- **THEN** `date` uses the earliest code/plan commit date (`git log
  --diff-filter=A`), not the GitHub issue date (none exists)

### Requirement: artifacts and cross-links

Each post SHALL include selectable YAML front matter:
`title`, `date`, `draft`, `tags`, `categories`, `summary`, `artifacts`
(openspec change paths, PR IDs, issue IDs), and `related` (sibling post
filenames) forming cross-links between posts. Posts SHALL link source artifacts
in the body rather than duplicating `docs/*` content (DRY).

#### Scenario: sibling dependency link
- **GIVEN** a post on topology (CRUSH) and a post on storage (CAS)
- **WHEN** either is authored
- **THEN** each lists the other in `related`, so the corpus navigates (e.g.
  CRUSH post links CAS post, P2P posts link lease/durability posts)

### Requirement: bolt and sentinel mindsets embedded

Posts where performance or security drove decisions SHALL tag and narrate the
⚡ Bolt and/or 🛡 Sentinel aspects, linking `docs/STANDARDS.md`, with `bolt`
and/or `sentinel` in `tags`. Posts that combine both MUST state the
perf/security tradeoff explicitly.

#### Scenario: security-driven post
- **GIVEN** a post on pentest findings, TLS enforcement, E2EE, or auth hardening
- **WHEN** authored
- **THEN** it carries the `sentinel` tag, narrates the security decision, and
  links `docs/PENTESTING.md` / `docs/STANDARDS.md` as relevant

#### Scenario: performance-driven post
- **GIVEN** a post on zero-allocation hot paths, benchstat gating, or profiling
- **WHEN** authored
- **THEN** it carries the `bolt` tag and links `docs/PERFORMANCE.md` /
  `docs/STANDARDS.md`

## UNCHANGED Behavior
- No change to source code, storage, protocol, transport, or CI build/test
  behavior (a new read-only post/validator workflow is additive; it does not
  gate builds).
- Post authorship does not replace OpenSpec changes; specs remain the source of
  truth for requirements, posts are the narrative/link layer (Rule 39).
- The same rules on three-dot diff, master merge, pre-commit hooks, and CI
  gates (Rules 50, 62, 71) apply unchanged.