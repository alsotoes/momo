# Source References (Go / Golang)

## Official Repository

- **Go**: https://github.com/approvals/go-approval-tests

## Key Source Files & Stdlib Alternatives

### `approvals/go-approval-tests`
- `approvals.go` - `VerifyString`, `VerifyJSONStruct`, `VerifyJSONBytes`, `VerifyMap`, `VerifyArray`, `VerifyAllCombinationsFor1`..`9`
- `scrubber.go` - `Options().WithRegexScrubber`, custom `Scrubber` functions (`func(string) string`)
- `namer.go` - `UseFolder`, `UseSubTestNames` for `t.Run` subtests

### Zero-Dependency Go Stdlib Golden Pattern
- `flag.Bool("update", false, "update .approved.txt golden files")`
- `testdata/<TestName>.approved.txt` (committed) and `testdata/<TestName>.received.txt` (gitignored on failure)
- `golang.org/x/tools/txtar` for multi-file input/output fixture archives

## Important Go Patterns

### Scrubbers
- GUIDs / UploadIDs → `<guid_1>`, `<guid_2>` (indexed replacement preserving identity)
- Dates (`RFC3339Nano`, `RFC1123`) → `<date_1>` or `<HTTP_DATE>`
- Ephemeral ports (`127.0.0.1:0`) → `127.0.0.1:<PORT>` or `<addr_1>`
- Regex → `regexp.MustCompile(pattern).ReplaceAllString` or `ReplaceAllStringFunc`
- Combining → chain `Scrubber` functions (`func(string) string`)

### Multiple Approvals Per Test
- Go: `t.Run("subtest", ...)` subtest names (`t.Name()`) or `approvals.UseSubTestNames()`

### Inline Approvals
- Go: Raw string literals (`` `...` ``) with `VerifyInline(t, actual, expected)`

### Structured Log Capture
- `log/slog`: `slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: ...})` dropping `slog.TimeKey`
- `log`: `log.SetOutput(&buf)` + `log.SetFlags(0)` restored via `t.Cleanup`
