# Change: Hugo blog under docs/blog — project journey, research, architecture decisions, changes

**Related Issues:**
- https://github.com/alsotoes/momo/issues/964 (tracking)
- https://github.com/alsotoes/momo/issues/928 (roadmap parent)

## Why

Momo's evolution from a file-replication playground to a distributed object
store (S3 gateway, CAS, P2P, R1–R3 durability, R4 momofs FUSE/POSIX) is buried
in ~45 ratified OpenSpec changes, git history, and CI artifacts. There is no
narrative trail for maintainers, contributors, or stakeholders. The engineering
journey — research, architecture decisions, the ⚡ Bolt (performance) and
🛡 Sentinel (security) mindsets, and how steering/governance evolved — is real
but undocumented as a story.

## What

1. **Steering Rule 76 (governance):** Ratified feature/enhancement OpenSpec
   changes MUST ship a matching Hugo-format blog post in `docs/blog/` (same PR
   or immediately-following PR) describing the change, its research,
   architecture decision, and Bolt/Sentinel implications. `no-blog`
   justification permitted only for internal-only changes with no narrative
   value. AI reviewer enforces (Rule 70).
2. **Content:** Hugo-format posts (markdown + YAML front matter) under
   `docs/blog/`, content-only (no hugo.toml/theme in repo).
3. **Implemented-state rule:** Posts MUST reflect implemented behavior, grounded
   in verified code (`src/`) and docs **outside** `docs/momofs/`. `docs/momofs/`
   is design/plan; citing un-implemented design as shipped == spec violation.
   Exceptions: operational docs (`MOUNT_USER_GUIDE.md`, `IMPLEMENTATION.md §2.3`)
   used only when ratified by matching `src/momofs` code.
4. **Post date rule:** `date` = `createdAt` of anchor GitHub issue/PR
   (`gh issue view <n> --json createdAt`); when a feature has no issue anchor,
   use the earliest code-implementation or plan commit date. No future-dating.
5. **Per-post artifacts + cross-links:** each post carries `artifacts`
   (openspec change dirs, PRs, issues) and `related` cross-links to sibling
   posts (e.g. CRUSH post ↔ CAS post), forming a navigable web.
6. **Bolt/Sentinel embedded:** each post where perf or security drove the design
   MUST tag and narrate the ⚡/🛡 aspects, linking `docs/STANDARDS.md`.

## Out of scope
- Building/publishing a Hugo site; adding hugo.toml/theme; generating posts
  automatically from specs (curated prose, not generated).
- Rewriting or duplicating `docs/*` design content (posts link, don't duplicate).
- Backfilling posts for historical pre-GitHub commits beyond the curated set.