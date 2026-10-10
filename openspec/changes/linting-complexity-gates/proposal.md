# Change: Pre-merge complexity gates (gocognit/gocyclo)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1172 (tracking)

## Why

Master's SonarCloud gate went red three times in a row after merges
(#1164 → #1167 → #1169), and every failure was the same rule: `go:S3776` —
cognitive complexity > 15 on new code. Each round surfaced only *after* the
merge, when the master pipeline turned red and required a follow-up PR.

## What Changes

- `.golangci.yml` (v2 schema): `gocognit` (the same cognitive-complexity metric
  Sonar uses for S3776) plus `gocyclo`, both at threshold 15, with
  `issues.new: true` so reporting is new-code-only and legacy hotspots stay
  grandfathered until a PR edits them.
- `.github/workflows/lint.yml`: PR-only `golangci-lint-action` job with
  `only-new-issues: true`, making the gate fire before merge.
- `Makefile`: `make lint` target for local runs before pushing.
- `docs/CORE/STANDARDS.md`: the complexity budget standard (new functions ≤ 15;
  never raise thresholds to silence findings; legacy grandfathering; every
  error branch tested with new-code coverage ≥ 80%).

## Impact

- **Pros:** complexity regressions are caught on the PR; `make lint` is a
  faithful local preview of the Sonar master gate (same metric, same threshold,
  same new-code semantics).
- **Cons / mitigations:** legacy offenders (HandshakeServer ~199, Daemon ~118)
  are grandfathered by `issues.new: true` — a full-repo legacy run finds 19
  violations in `src/storage` alone, and the configured new-code mode finds 0
  on master today, so the gate lands green and only bites future regressions.
