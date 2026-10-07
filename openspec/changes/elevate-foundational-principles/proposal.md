# Proposal: Elevate Foundational Principles and Adaptive Architecture from momofs

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1138

- **Champion:** alsotoes
- **Status:** `Proposed`

## 1. Problem

Momo's foundational design principles and biological adaptive system models are currently buried inside `docs/momofs/`, a subdirectory historically associated with the FUSE/POSIX filesystem layer (`momofs`).

As a consequence:
1. These system-wide principles are omitted from the primary documentation index (`docs/README.md`).
2. They are unreferenced in the core architecture guide (`docs/ARCHITECTURE.md`).
3. AI agent instructions (`AGENTS.md`) and steering rules (`openspec/config.yaml`) do not mandate reviewing them before proposing changes.
4. Rule 76 in `openspec/config.yaml` describes `docs/momofs/` as "unshipped design", inadvertently causing developers and AI agents to disregard the authoritative principles within it.

## 2. Proposed Solution

1. **Promote Foundational Principles to Top-Level Documentation (`docs/`)**:
   - `docs/CORE/DESIGN_PRINCIPLES.md`: 14 Core Principles (Read From Any Node, Zero SPOF, Self-Healing, HPC Ready, Cloud Ready...)
   - `docs/CORE/ADAPTIVE_SYSTEMS.md`: 16 Biological Principles (Ant Colony, Epigenetics, Homeostasis, Stigmergy, Immune System...)
   - `docs/CORE/DESIGN_DECISIONS.md`: Foundational Decisions DD-1 to DD-6
   - `docs/CORE/COMPARISON.md`: Industry comparisons vs Ceph, Lustre, ScyllaDB, IPFS
   - `docs/CORE/LESSONS_LEARNED.md`: Actionable patterns from distributed storage systems
2. **Preserve Link Integrity**:
   - Maintain forward pointer stubs in `docs/momofs/` pointing to the elevated documents in `docs/` so no existing links break.
3. **Primary Indexing (`docs/README.md`)**:
   - Add a prominent "🏛️ Foundational Principles & Architecture" section at the top of `docs/README.md` and complete table entries.
4. **Architecture Grounding (`docs/ARCHITECTURE.md`)**:
   - Weave the 14 core and 16 biological principles directly into `docs/ARCHITECTURE.md`.
5. **Agent Onboarding (`AGENTS.md` & `openspec/config.yaml`)**:
   - Require all AI agents and contributors to ground architectural proposals in `docs/CORE/DESIGN_PRINCIPLES.md` and `docs/CORE/ADAPTIVE_SYSTEMS.md`.

## 3. Scope & Boundaries

- **In Scope:** Elevating system-wide principles, creating forward stubs, updating documentation indices, and updating agent steering rules.
- **Out of Scope:** Altering Go application code, wire protocols, or public APIs.
