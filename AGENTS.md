# AGENTS.md

All project steering rules and engineering standards are defined in **one** primary file:
[`openspec/config.yaml`](openspec/config.yaml) (under the `context` block, "Project Steering Rules").

This file is the single source of truth (Rule 39). Do not duplicate rules here.
All AI agents (Gemini, opencode, Jules, etc.) MUST reference `openspec/config.yaml` for steering rules.

## Architectural Philosophy & Core Principles

Before proposing architectural shifts, authoring new specifications, or refactoring subsystems, all AI agents MUST review and align with Momo's foundational design documents:
- **Core Design Principles**: [`docs/CORE/DESIGN_PRINCIPLES.md`](docs/CORE/DESIGN_PRINCIPLES.md) (14 tenets centered on "Read From Any Node" and zero single point of failure)
- **Adaptive Systems Design**: [`docs/CORE/ADAPTIVE_SYSTEMS.md`](docs/CORE/ADAPTIVE_SYSTEMS.md) (16 biological models including Ant Colony routing, Epigenetics, Homeostasis, Stigmergy)
- **Engineering Standards**: [`docs/CORE/STANDARDS.md`](docs/CORE/STANDARDS.md) (⚡ Bolt zero-allocation performance & 🛡️ Sentinel defensive security)
- **Architectural Decisions**: [`docs/CORE/DESIGN_DECISIONS.md`](docs/CORE/DESIGN_DECISIONS.md) (DD-1 to DD-6 trade-offs)
