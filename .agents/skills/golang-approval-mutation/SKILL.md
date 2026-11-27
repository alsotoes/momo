---
name: golang-approval-mutation
description: "Golang approval testing (golden files / snapshot testing) and mutation testing. Combines `.approved.txt`/`.received.txt` or `testdata/*.golden` characterization tests with Go-specific mutation analysis (`gremlins`, boundary/errno/panic-recovery/concurrency mutants). Use when characterizing complex Go outputs (S3 XML/JSON responses, wire protocol payloads, CRUSH placement tables, state transitions) or verifying that Go tests kill mutants on changed code."
user-invocable: true
license: MIT
---

STARTER_CHARACTER = 📸🧬

# Go (Golang) Approval & Mutation Testing

This skill unites **Approval Testing (Golden Master / Snapshot Testing)** and **Mutation Testing** for Go codebases. Together they form a high-leverage verification loop:
- **Approval Tests** capture rich, multi-field observable behavior (S3 XML responses, CRUSH placement tables, state-machine storyboards, combinatorial matrices) in human-scannable `testdata/*.approved.txt` files.
- **Mutation Testing** proves those approval snapshots and unit assertions are **behavioral and sensitive** — verifying that any subtle bug introduced into production Go code (off-by-one boundary, wrong `syscall.Errno`, dropped `err != nil`, missing `common.RecoverErrClose`) causes a test to fail.

For full reference details, also consult:
- [`.agents/skills/approval-tests/golang.md`](../approval-tests/golang.md) — Go stdlib `-update` golden helper, `approvals/go-approval-tests`, scrubbers, and combinations.
- [`.agents/skills/approval-tests/references/golang/api.md`](../approval-tests/references/golang/api.md) — `VerifyHTTPResponse` (`httptest`), `VerifyXML` (S3 XML), `VerifyBytesHexDump` (binary wire frames), `MarkdownTable`, and `txtar` fixtures.
- [`.agents/skills/approval-tests/references/golang/scrubbers.md`](../approval-tests/references/golang/scrubbers.md) — Determinism traps (Go random map order), indexed GUID/RFC3339/UnixNano/ephemeral-port scrubbers, `t.TempDir()` path scrubbing.
- [`.agents/skills/approval-tests/references/golang/patterns.md`](../approval-tests/references/golang/patterns.md) — `log/slog` & `log` capture, inline raw-string approvals, characterization testing before refactoring, and Go CI troubleshooting.
- [`.agents/skills/mutation-testing/languages/golang.md`](../mutation-testing/languages/golang.md) — Go-specific mutation operators (`err != nil`, `errors.Is`, `syscall.*`, `defer recover()`, slice bounds, bitwise, concurrency, type assertions, equivalent mutants, and `testing.AllocsPerRun`).
- [`.agents/skills/mutation-testing/tools/golang.md`](../mutation-testing/tools/golang.md) — `gremlins`, `go-mutesting`, GitHub Actions `--diff` CI gate, and zero-install surgical mutant verification.

---

## Workflow 1: Approval Testing in Go

Use approval tests when output is structural, multi-line, or combinatorial:
- **S3 / HTTP Wire Payloads**: XML responses (`ListObjectsV2`, `GetObjectTagging`, `InitiateMultipartUpload`, S3 Error XML), HTTP headers.
- **CRUSH & Routing Matrices**: `VerifyAllCombinations` across `(nodeCount, replicationFactor, failureDomain)` in one snapshot.
- **State Progressions (Storyboard)**: Multi-step replication tier shifts, multipart upload manifest recovery lifecycle, or gossip state convergence.

### Golden Rules for Go Approvals

1. **Store in `testdata/`**: Go's toolchain ignores `testdata/` during compilation and sets cwd to the package directory during `go test`.
2. **Scrub Non-Determinism First**: Before first approval, apply scrubbers for:
   - Ephemeral ports (`127.0.0.1:\d+` → `127.0.0.1:<PORT>`, per Momo Rule 40)
   - Timestamps (`RFC3339Nano`, Unix nano timestamps → `<TIMESTAMP>`)
   - Random IDs / UploadIDs / UUIDs → indexed `<id_1>`, `<id_2>` (preserving equality relationships)
3. **Never Hand-Edit `.approved.txt`**: Generate via `go test ./... -update` (or copy `.received.txt` to `.approved.txt` after human inspection).
4. **Ignore `.received.txt` in Git**: Keep `*.received.*` in `.gitignore`; commit `testdata/*.approved.txt`.

---

## Workflow 2: Mutation Testing in Go

After tests pass (GREEN), challenge the test suite against Go-specific mutation operators:

### High-Priority Go Mutants to Kill

| Target | Original | Mutant | How to Kill in Go Tests |
|--------|----------|--------|-------------------------|
| **Boundary Check** | `len(b) > 64` | `len(b) >= 64` | Test exact boundary `len(b) == 64` AND `len(b) == 65` |
| **POSIX Error Mapping** | `syscall.EACCES` | `syscall.EINVAL` | Assert `errors.Is(err, syscall.EACCES)`, never just `err != nil` |
| **Error Wrapping** | `fmt.Errorf("%w", err)` | `fmt.Errorf("%v", err)` | Unwrap with `errors.Is` / `errors.As` |
| **Panic Recovery** | `defer common.RecoverErrClose("op", &err, c)` | `defer common.RecoverErr("op", &err)` | Inject panicking mock `net.Conn`, assert `errors.Is(err, syscall.EIO)` AND `c.Closed == true` |
| **Amortized Syscall** | `count&127 == 0` | `count&127 != 0` | Assert exact deadline syscall count across 129 writes (must be 2, not 127) |
| **Map Lookup** | `v, ok := m[k]; if !ok` | `if m[k] == 0` | Test key present in map with explicit zero value `0` / `""` |
| **Context / Timeout** | `case <-ctx.Done():` | (removed) | Pass pre-cancelled `ctx` (or `testing/synctest`), assert `context.Canceled` |

### Running Mutation Analysis on a Branch

1. **Automated (`gremlins`):**
   ```bash
   gremlins unleash --diff "master" ./src/<pkg>
   ```
2. **Surgical Verification (Zero-Install):**
   - Inspect `rtk git diff master...HEAD -- src/`.
   - For each conditional, error return, bitmask, or recovery block added/modified, verify a subtest fails if that line is mutated.
   - Strengthen table-driven tests or approval combination inputs until 100% of non-equivalent mutants on changed lines are killed.
