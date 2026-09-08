# Change: Upgrade to Go 1.26

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1064 (tracking)

## Why

Go 1.25 reaches end-of-life soon, and the quic-go 0.62.0 release (which we need for RFC 9218 stream priorities and other fixes) requires Go 1.26+. Upgrading to Go 1.26 keeps us on a supported release and unblocks the quic-go upgrade (PR #1060). Go 1.26 also brings performance improvements (PGO, better ARM64 codegen, faster builds) and security fixes.

## What Changes

- **Root go.mod / go.work**: `go 1.25.10` → `go 1.26.x`
- **All 14 workflow files** using `go-version: '1.25.x'` → `'1.26.x'`
- **verify_go_version.yml**: Update version check logic to accept 1.26
- **All module go.mod files**: Sync to 1.26 via `go work sync`
- **CI validation**: Trigger full CI to verify benchstat, blog-check, adr-sync

## Non-Goals

- No Go 1.26-specific language features used in code (backward compatible)
- No changes to the quic-go version (will be handled in separate PR after 1.26 merge)

## Impact

- **Performance**: Go 1.26 PGO and ARM64 codegen improvements
- **Security**: Go 1.25 is approaching EOL; 1.26 has latest security fixes
- **Dependencies**: Unblocks quic-go 0.62+ (RFC 9218 priorities, Safari WebTransport compat)
- **CI**: Must pass benchstat regression gate (pinned x/perf) and blog-check/adr-sync

## Rollback Plan

If critical regression: revert go.mod/go.work to 1.25.10, revert workflow files, re-pin quic-go 0.61.0 in dependabot.