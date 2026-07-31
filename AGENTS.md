# AGENTS.md

All project steering rules and engineering standards are defined in **one** primary file:
[`openspec/config.yaml`](openspec/config.yaml) (under the `context` block, "Project Steering Rules").

This file is the single source of truth (Rule 39). Do not duplicate rules here.
All AI agents (Gemini, opencode, Jules, etc.) MUST reference `openspec/config.yaml` for steering rules.
