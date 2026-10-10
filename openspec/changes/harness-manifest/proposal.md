# Change: Harness manifest + validation gate (ai-workflow.yaml)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1174

## Why

Today 6 global plugins + 1 project plugin run in opencode with no inventory, no pinning policy, no validation against misuse (e.g. Ponytail `ultra` could silently override our Rule 37 ceremony). No single source of truth exists for "how we work with agents/plugins" — only scattered docs and implicit knowledge.

## What Changes

1. **Manifest** (`openspec/ai-workflow.yaml`): canonical inventory of every agent + plugin that touches the repo — role, scope, pinned version, allowed/forbidden behaviors referencing steering-rule IDs.
2. **Validator** (`make ai-workflow-check`): CI gate that enforces the manifest — declares vs actual plugins, pinning, forbidden modes, token leakage, rule-ID cross-references, global config consistency.
3. **Config**: Ponytail enabled in project `opencode.json` (`@dietrichgebert/ponytail@5.1.0`, `lite` default, `ultra` forbidden).
3. **Docs**: `AGENTS.md` + `AI_FLYING_SOLO.md` point to the manifest; `docs/CORE/STANDARDS.md` documents the harness standard.
4. **Validator** (`make ai-workflow-check` + CI): 7 checks — declared vs actual plugins, pinning, forbidden modes, token leaks, rule-ID cross-refs, global config consistency, Ponytail mode policy.
