# ECC Tools Integration

How momo uses the **ECC Tools** GitHub App, and how its advisory audits are wired into
momo's own automation.

## What ECC Tools is

ECC Tools (`ecc-tools[bot]`) is the hosted GitHub App for the ECC project
([ecc.tools](https://ecc.tools), marketplace `github.com/marketplace/ecc-tools`). It
analyzes repository history and agent configuration, runs **advisory PR audits**, and can
open pull requests with repo-specific "ECC bundle" outputs (skills, rules, hooks,
manifests). The app is installed on this repository only.

Public repository → free tier (10 analyses/month, up to 200 commits per run).

## Repository configuration

`.github/ecc-tools.yml` declares the repo-scoped intent:

- **Excludes** the analyzer from `vendor/**`, `docs/blog/**`, `docs/html/**`, generated
  benchmark dumps, and `repomix-output.*`.
- **`autoPR.enabled: false`** — the app does not open bundle PRs on push. Onboarding is
  explicit, via the `/ecc-tools setup` and `/ecc-tools analyze` comment commands.

> The `.github/ecc-tools.yml` schema originates from an earlier ECC app generation; it is
> kept as the declared intent and verified against the app's actual behaviour.

## Advisory PR audits

On every PR push the app posts one comment per audit dimension. Each carries a marker:

```
<!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->
```

Dimensions seen in practice: **Security Evidence**, **PR Risk Taxonomy**, **Reference Set
Readiness**, **Hosted Promotion Readiness** (plus **PR Config Audit** / **PR Harness Audit**
when the diff touches config or harness files). Each verdict is a bolded phrase with a
status, e.g. `**Security evidence gate passed** (success)`.

The app cannot publish these as check runs today — it does not declare `checks:write`, so it
falls back to comments. A request to change that is filed upstream:
[ECC-Tools/.github#6](https://github.com/ECC-Tools/.github/issues/6).

## Integration 1 — reviewer surfaces the audits (advisory)

`.github/scripts/ai_reviewer.py`:

- `parse_ecc_audit_summary(comments)` — pure parser that reads the markers, keeps the
  **latest verdict per dimension**, and maps status → ✅ success / ⚠️ neutral / ❌ failure.
- `get_ecc_audit_summary(pr_number)` — fetches the PR comments and calls the parser.
- The summary is appended to the posted review **after** the approve decision
  (`is_approved`) is computed, so it is **advisory and never changes merge gating**.

Example appended block:

```
---
**ECC audit** (advisory) — Security Evidence: ✅ Security evidence gate passed ·
PR Risk Taxonomy: ⚠️ PR taxonomy review recommended · Reference Set Readiness: ⚠️
Reference set readiness gaps detected · Hosted Promotion Readiness: ✅ Hosted promotion
readiness passed
```

Unit tests: `.github/scripts/test_ai_reviewer_ecc.py`.

## Integration 2 — auto-triage ECC bundle PRs

`.github/workflows/ecc_bundle_triage.yml` closes unsolicited "ECC bundle" PRs opened by
`ecc-tools[bot]`, posting a rationale first. This is defense-in-depth for
`autoPR.enabled: false`: the generated bundles target harnesses momo does not use
(`.claude/` Claude Code, `.codex/` Codex), while momo runs OpenCode with a curated
`.agents/skills/` set.

## Governance

ECC Tools is **advisory**. It does not gate merges: the AI reviewer (Rule 70) and the
merge gate (Rule 55) remain authoritative, and ECC's verdicts are never part of
`is_approved`. See ADR 0064 and the OpenSpec change `ecc-tools-reviewer-integration/`.

## Tracking

- Setup: [#1125](https://github.com/alsotoes/momo/issues/1125)
- Reviewer integration + bundle triage: [#1134](https://github.com/alsotoes/momo/issues/1134)
- Upstream `checks:write` request: [ECC-Tools/.github#6](https://github.com/ECC-Tools/.github/issues/6)
