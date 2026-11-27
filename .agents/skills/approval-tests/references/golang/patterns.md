# Go Approval Testing Patterns: Logging, Inline, Combinations & CI

---

## 1. Capturing & Approving Structured Logs (`log/slog` and `log`)

### Standard `log/slog` Capture (Go 1.21+)

To approve structured logs deterministically, route `slog` to a `bytes.Buffer` and strip the built-in `time` attribute via `ReplaceAttr`:

```go
func CaptureSlog(fn func(logger *slog.Logger)) string {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{} // drop non-deterministic wall clock timestamp
			}
			return a
		},
	})
	fn(slog.New(handler))
	return buf.String()
}

func TestReplicationFailoverLogs(t *testing.T) {
	logs := CaptureSlog(func(logger *slog.Logger) {
		runFailoverScenario(logger)
	})
	Verify(t, logs, ScrubLoopbackPorts)
}
```

### Standard Library `log.Printf` Capture

When legacy code writes to package-level `log.Printf` (with flags like `log.LstdFlags`), zero out the date/time flags during the test and capture output into a buffer:

```go
func CaptureStdLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0) // deterministic: omit Ldate | Ltime
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	})
	fn()
	return buf.String()
}
```

---

## 2. Inline Approvals in Go

When the expected snapshot is short (5–15 lines) and creating an external file in `testdata/` adds clutter, use Go raw string literals (backticks `` `...` ``) with a unified diff helper:

```go
func VerifyInline(t *testing.T, actual, expected string, scrubbers ...Scrubber) {
	t.Helper()
	for _, scrub := range scrubbers {
		actual = scrub(actual)
	}
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual != expected {
		t.Fatalf("inline approval mismatch:\n--- EXPECTED\n%s\n+++ ACTUAL\n%s", expected, actual)
	}
}

func TestFormatCRUSHPlacement(t *testing.T) {
	got := formatPlacementTable(cluster, "object-key-1")
	VerifyInline(t, got, `
Object: object-key-1
Primary:   node-2 (zone-c)
Secondary: node-0 (zone-a)
Tertiary:  node-1 (zone-b)
`)
}
```

---

## 3. Characterization Testing Before Refactoring

When refactoring a complex Go function (e.g. splitting a high-cognitive-complexity S3 handler to satisfy Sonar S3776 / `gocognit` ≤ 15):
1. Write a combinatorial approval test covering all valid and invalid request permutations (`method`, `queryParams`, `headers`, `bodyState`).
2. Run `go test -run TestS3HandlerCharacterization -update` once to lock in the `.approved.txt` baseline.
3. Extract helpers and simplify control flow.
4. Run `go test -run TestS3HandlerCharacterization` (without `-update`) after every step — any unintended behavioral change shows up immediately as a line diff against `.approved.txt`.

---

## 4. Go CI Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Passes locally, fails in CI with `\r\n` vs `\n` | Windows checkout converted `.approved.txt` to CRLF | Add `*.approved.* text eol=lf` to `.gitattributes` and normalize `strings.ReplaceAll(s, "\r\n", "\n")` in `Verify` |
| Order of items in output changes randomly between runs | Ranging over a `map[K]V` or collecting results from concurrent goroutines without sorting | Sort keys with `slices.Sort` before formatting, or sort collected channel results by deterministic ID |
| Error message differs between macOS and Linux | Raw OS error string in output (`connection refused` vs `connectex`) | Format using portable `syscall.Errno` name or scrub OS-specific error suffixes |
| Parallel subtests overwrite the same approval file | Calling `Verify` inside a loop without `t.Run(tt.name, ...)` | Wrap each scenario in `t.Run(tt.name, func(t *testing.T) { Verify(t, ...) })` so `t.Name()` is unique |
