# Spec: Harness manifest + validation gate

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1174

## Requirements

### HWC-A1: Single source of truth for agents/plugins

**Requirement:** The repository MUST contain a single YAML manifest (`openspec/ai-workflow.yaml`) that declaratively lists every agent and plugin interacting with the codebase, with: role, scope (`project`|`global`), pinned version (or declared floating), allowed/forbidden behaviors keyed by steering-rule IDs.

### HWC-A2: Validation gate on every PR

**Requirement:** A `make ai-workflow-check` target MUST exist and run on every PR, failing the build if:
- Any plugin in `opencode.json` (project + parsed global) is undeclared in the manifest.
- A declared `pinned` version uses `@latest` or unpinned specifier.
- `ponytail` is allowed `ultra` mode, or lacks `default_mode: lite`, or `ultra` is listed as allowed.
- `opencode.json` or manifest contains hardcoded tokens/secrets.
- Declared config files (`.github/ecc-tools.yml`, etc.) are missing.
- Referenced steering-rule IDs don't exist in `openspec/config.yaml`.
- Global MCP set (`mempalace`) ≠ declared global set.
- `ponytail.default_mode` ≠ `lite` or `ultra` is listed in allowed.

### HWC-A3: Manifest documents Ponytail with strict policy

**Requirement:** The manifest MUST declare Ponytail with:
- `pinned: "5.1.0"` (or explicit version)
- `default_mode: "lite"`; `ultra` MUST be in `forbidden`
- `allowed: ["diff-review", "minimalism-guidance"]`; `forbidden: ["ultra-mode", "skipping-openspec-ceremony", "replacing-lint-gate"]`
- `scope: project` (project-local install)

### HWC-A4: Configuration files consistent with manifest

**Requirement:** Project `opencode.json` MUST declare exactly the plugins listed in the manifest with matching versions/scopes. No undeclared plugins, no unpinned versions unless explicitly `floating: true` in the manifest.

### HWC-A5: Referenced rules exist

**Requirement:** Every `rule` or `forbidden` reference in the manifest MUST resolve to an existing rule ID in `openspec/config.yaml`.

### HWC-A6: Global plugins inventoried

**Requirement:** All 6 global plugins MUST be declared with `scope: global` and their roles documented.

### HWC-A7: Documentation

**Requirement:** `AGENTS.md` + `AI_FLYING_SOLO.md` + `docs/README.md` MUST reference `openspec/ai-workflow.yaml` as the authoritative "how we work with agents/plugins" surface.
