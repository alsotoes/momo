# Architecture Decision Records

This directory holds the project's **Architecture Decision Records (ADRs)** following the
[Fowler/Nygard](https://adr.github.io/) pattern. Each ADR captures a single architectural
decision, the problem it solves, the trade-offs considered, and its current implementation
status.

## What an ADR is

An ADR is a short, immutable record of a decision: *what we decided, why, and what it
cost us*. ADRs are **decision records** — the "why" history of the project. The living
reference documentation (`docs/ARCHITECTURE.md`, `docs/CONFIGURATION.md`, etc.) is the
*current state*; ADRs are the *decision log* that produced it.

## How ADRs are generated (Rules 77/78)

ADRs are **auto-generated** from the OpenSpec change sources by:

```bash
make adr-sync        # regenerate all ADRs from openspec/changes/
make adr-sync-check  # verify ADR ↔ spec parity (runs in CI, blocking on mismatch)
```

Each ADR maps 1:1 to a directory under `openspec/changes/<change-id>/` (excluding
`archive/`), numbered by the alphabetical index of that directory (`0001`, `0002`, ...):

| ADR section | Source |
|---|---|
| `Status` | `tasks.md` checkbox completion → `Accepted` (all done) / `Proposed` (partial) |
| `Confidence` | task completion ratio → High (≥90%) / Medium (≥50%) / Low |
| `Context` | `proposal.md` "Why" / "Problem" / "Problem Statement" (fallback: spec "Purpose") |
| `Decision` | `specs/*/spec.md` requirement summaries (SHALL statements) |
| `Consequences` | spec "Consequences" / "Impact" / "Trade-offs" |
| `Alternatives Considered` | spec "Alternatives" / "Options" |
| `Implementation Status` | `tasks.md` checkboxes grouped into Code / Tests / Docs |
| `References` | Issue (spec `GitHub Issue URL`), PR + Blog (matched blog post artifacts) |

## Rules

- **Never hand-edit an ADR** — regenerate it with `make adr-sync` (Rule 78). Any
  spec/tasks change requires regenerating the ADR in the same commit.
- **Immutable once Accepted** — do not modify an Accepted ADR. To change a decision,
  write a new ADR that marks the old one `Deprecated` (Fowler supersession, Rule 77).
- **Every ratified feature/enhancement needs an ADR** (Rule 77). Bugfixes with no
  behavioral surface are exempt.
- **Blog coverage** (Rule 76): Accepted ADRs must have a matching blog post (or a
  documented `no-blog` justification), enforced by `make blog-check`.

## Index

| ADR | Decision | Status | Issue |
|---|---|---|---|
| 0001 | Adaptive per-source auth backoff | ... | ... |
| 0002 | Adaptive gossip scale | ... | ... |
| ... | ... | ... | ... |

> The full index is the sorted `docs/adr/NNNN-<change-id>.md` listing itself; the
> `References` block in each ADR links its issue, PR, spec, and blog post.

## Lifecycle

1. **Proposed** — the OpenSpec change exists; implementation partial or not started.
2. **Accepted** — all `tasks.md` checkboxes checked (code + tests + docs shipped).
3. **Deprecated** — superseded by a newer ADR (the new ADR links `Supersedes:`).

See `docs/adr/template.md` for the canonical structure and source mapping.