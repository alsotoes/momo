---
description: Execute a feature or bugfix following the AI Flying Solo workflow
---

Task: $ARGUMENTS

1. **Guidelines:** Read `docs/AI_FLYING_SOLO.md` and the steering rules in `openspec/config.yaml`, and follow them strictly. (If MemPalace is available, query `AI_FLYING_SOLO` for additional grounding.)
2. **Context:** Inspect `repomix-output.xml` (or run `npx repomix` if missing/outdated) to map the existing architecture, directory structure, and interfaces.
3. **Plan:** Outline your proposed implementation, target file paths, and testing strategy.
4. **Implementation:** Create/edit files directly, ensure clean LSP diagnostics, and execute tests.
5. **Finalize:** Provide the commit message and PR summary matching our workflow template.
