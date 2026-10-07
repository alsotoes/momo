# 0068-elevate-foundational-principles

## Status
Accepted

## Confidence
High

## Context
Momo's foundational design principles and biological adaptive system models are currently buried inside `docs/momofs/`, a subdirectory historically associated with the FUSE/POSIX filesystem layer (`momofs`).

As a consequence:
1. These system-wide principles are omitted from the primary documentation index (`docs/README.md`).
2. They are unreferenced in the core architecture guide (`docs/ARCHITECTURE.md`).
3. AI agent instructions (`AGENTS.md`) and steering rules (`openspec/config.yaml`) do not mandate reviewing them before proposing changes.
4. Rule 76 in `openspec/config.yaml` describes `docs/momofs/` as "unshipped design", inadvertently causing developers and AI agents to disregard the authoritative principles within it.

## Decision
- Top-Level Principles Promotion: The documentation repository SHALL provide foundational, system-wide architectural documents directly under `docs/`: 1. `docs/DESIGN_PRINCIPLES.md` (14 Core Principles) 2. `docs/ADAPTIVE_SYSTEMS.md` (16 Biological Adaptive Models) 3. `docs/DESIGN_DECISIONS.md` (Foundational Decisions DD-1 to DD-6) 4. `docs/COMPARISON.md` (System Comparison vs Ceph, Lustre, ScyllaDB, IPFS) 5. `docs/LESSONS_LEARNED.md` (Actionable Patterns from Distributed Storage)
- MomoFS Forward Compatibility Stubs: The `docs/momofs/` directory SHALL retain backward-compatible pointer stubs for all relocated files (`docs/momofs/DESIGN_PRINCIPLES.md`, `docs/momofs/ADAPTIVE_SYSTEMS.md`, `docs/momofs/DESIGN_DECISIONS.md`, `docs/momofs/COMPARISON.md`, `docs/momofs/LESSONS_LEARNED.md`) that direct readers to the top-level paths.
- Main Documentation Indexing: `docs/README.md` SHALL contain a prominent "🏛️ Foundational Principles & Architecture" section above the general index table, with direct links and descriptions for `DESIGN_PRINCIPLES.md`, `ADAPTIVE_SYSTEMS.md`, `STANDARDS.md`, `DESIGN_DECISIONS.md`, `COMPARISON.md`, and `momofs/README.md`.
- Architecture and Agent Guidance Linkage: `docs/ARCHITECTURE.md`, `AGENTS.md`, and `openspec/config.yaml` SHALL explicitly link to `docs/DESIGN_PRINCIPLES.md` and `docs/ADAPTIVE_SYSTEMS.md` as required reading for system architecture and feature proposals.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1138
- PR: 
- Spec: `openspec/changes/elevate-foundational-principles/`
- Blog: 

