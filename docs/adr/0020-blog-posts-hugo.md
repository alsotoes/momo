# 0020-blog-posts-hugo

## Status
Accepted

## Confidence
High

## Context
Momo's evolution from a file-replication playground to a distributed object
store (S3 gateway, CAS, P2P, R1–R3 durability, R4 momofs FUSE/POSIX) is buried
in ~45 ratified OpenSpec changes, git history, and CI artifacts. There is no
narrative trail for maintainers, contributors, or stakeholders. The engineering
journey — research, architecture decisions, the ⚡ Bolt (performance) and
🛡 Sentinel (security) mindsets, and how steering/governance evolved — is real
but undocumented as a story.

## Decision
- steering rule 76 — post-per-ratified-change: The system (all AI agents) SHALL maintain posts under `docs/blog/posts/` in Hugo static-site format (markdown with YAML front matter), and the governing rules in `openspec/config.yaml` SHALL include Rule 76 requiring feature and enhancement PRs to ship a matching blog post in the same PR or an immediately-following PR.
- implemented-state grounding: Posts MUST describe implemented behavior. Content MUST be verified against `src/` code and documents under `docs/` outside `docs/momofs/`. The `docs/momofs/` directory is a design/plan suite and SHALL NOT be cited as shipped unless the matching implementation exists in `src/momofs` (`MOUNT_USER_GUIDE.md` and `IMPLEMENTATION.md` §2.3 are the operational exceptions, ratified by `src/momofs`).
- post date from issue/PR creation: Each post SHALL carry a `date` front-matter value equal to the `createdAt` timestamp of its anchor GitHub issue or pull request, queried via `gh issue view <N> --json createdAt` / `gh pr view <N> --json createdAt`. Features without an issue anchor SHALL use the earliest code-implementation or plan commit date derived from git history. Posts SHALL NOT be future-dated.
- artifacts and cross-links: Each post SHALL include selectable YAML front matter: `title`, `date`, `draft`, `tags`, `categories`, `summary`, `artifacts` (openspec change paths, PR IDs, issue IDs), and `related` (sibling post filenames) forming cross-links between posts. Posts SHALL link source artifacts in the body rather than duplicating `docs/*` content (DRY).
- bolt and sentinel mindsets embedded: Posts where performance or security drove decisions SHALL tag and narrate the ⚡ Bolt and/or 🛡 Sentinel aspects, linking `docs/STANDARDS.md`, with `bolt` and/or `sentinel` in `tags`. Posts that combine both MUST state the perf/security tradeoff explicitly. ## UNCHANGED Behavior - No change to source code, storage, protocol, transport, or CI build/test behavior (a new read-only post/validator workflow is additive; it does not gate builds). - Post authorship does not replace OpenSpec changes; ...

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/blog-posts-hugo/
- Blog: docs/blog/posts/...md
