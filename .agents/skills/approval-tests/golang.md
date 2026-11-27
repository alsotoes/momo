# Go (Golang) ApprovalTests & Golden Files

In Go, approval testing (golden master / snapshot testing) takes two idiomatic forms:
1. **Zero-Dependency Stdlib Golden Files (`testdata/*.approved.txt` or `testdata/*.golden`)** — preferred when a project enforces zero unnecessary external dependencies (like Momo's Rule 1).
2. **`github.com/approvals/go-approval-tests`** — the official ApprovalTests port for Go, providing `approvals.VerifyString`, `VerifyJSONStruct`, `VerifyAllCombinationsFor*`, and regex scrubbers out of the box.

---

## Option A: Zero-Dependency Stdlib Approval Helper (Recommended for Go)

Go's `go test` tool automatically ignores directories named `testdata/` during package compilation and sets the working directory to the package under test. A 40-line helper gives you full `.approved.txt` / `.received.txt` (or `-update` flag) semantics with **zero external dependencies**.

### Drop-in Helper (`approval_test.go`)

```go
package mypkg_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateApprovals = flag.Bool("update", false, "approve/update .approved.txt golden files")

type Scrubber func(string) string

// Verify compares actual against testdata/<TestName>.approved.txt.
// On mismatch (or missing approved file), writes testdata/<TestName>.received.txt and fails.
// Run `go test ./... -update` to promote received output to .approved.txt.
func Verify(t *testing.T, actual string, scrubbers ...Scrubber) {
	t.Helper()
	for _, scrub := range scrubbers {
		actual = scrub(actual)
	}
	if !strings.HasSuffix(actual, "\n") {
		actual += "\n"
	}

	safeName := strings.ReplaceAll(t.Name(), "/", ".")
	dir := "testdata"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	approvedPath := filepath.Join(dir, safeName+".approved.txt")
	receivedPath := filepath.Join(dir, safeName+".received.txt")

	if *updateApprovals {
		if err := os.WriteFile(approvedPath, []byte(actual), 0o644); err != nil {
			t.Fatalf("write approved: %v", err)
		}
		_ = os.Remove(receivedPath)
		return
	}

	expectedBytes, err := os.ReadFile(approvedPath)
	if err != nil {
		_ = os.WriteFile(receivedPath, []byte(actual), 0o644)
		t.Fatalf("missing approved file %s (wrote %s; review and run `go test -update` or copy .received.txt to .approved.txt)", approvedPath, receivedPath)
	}

	expected := string(expectedBytes)
	if actual != expected {
		_ = os.WriteFile(receivedPath, []byte(actual), 0o644)
		t.Fatalf("approval mismatch for %s:\n--- APPROVED (%s)\n%s\n+++ RECEIVED (%s)\n%s\nTo approve: cp %s %s (or `go test -run %s -update`)",
			t.Name(), approvedPath, expected, receivedPath, actual, receivedPath, approvedPath, t.Name())
	}
	_ = os.Remove(receivedPath)
}

// VerifyAsJSON marshals v with 2-space indentation before calling Verify.
func VerifyAsJSON(t *testing.T, v any, scrubbers ...Scrubber) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("VerifyAsJSON encode: %v", err)
	}
	Verify(t, buf.String(), scrubbers...)
}
```

---

## Option B: Official `go-approval-tests` Library

### Installation

```bash
go get github.com/approvals/go-approval-tests
```

### Quick Start

```go
package report_test

import (
	"testing"

	approvals "github.com/approvals/go-approval-tests"
	"github.com/approvals/go-approval-tests/scrubber"
)

func TestMain(m *testing.M) {
	approvals.UseFolder("testdata")
	m.Run()
}

func TestGenerateReport(t *testing.T) {
	result := generateReport()
	approvals.VerifyString(t, result)
}
```

First run fails and creates:
- `testdata/report_test.TestGenerateReport.received.txt` — actual output
- `testdata/report_test.TestGenerateReport.approved.txt` — empty expectation file

Review `.received.txt`; if correct, copy to `.approved.txt` (or run with `-update` in stdlib mode). Rerun → passes.

---

## Core Patterns in Go

### 1. `VerifyJSONStruct` / `VerifyAsJSON` — Structured Data & S3 XML/JSON Payloads

```go
func TestS3ListObjectsResponse(t *testing.T) {
	resp := buildListObjectsV2Result("my-bucket", items)
	approvals.VerifyJSONStruct(t, resp)
}
```

### 2. Scrubbing Timestamps, UUIDs, Ephemeral Ports (`127.0.0.1:0`), and Hashes

In Go tests—especially distributed systems and network tests binding ephemeral ports (`127.0.0.1:0`, Momo Rule 40) or RFC3339Nano timestamps—always scrub dynamic values **before** comparison:

```go
var (
	reRFC3339 = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z`)
	reUUID    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reAddr    = regexp.MustCompile(`127\.0\.0\.1:\d+`)
)

func ScrubDates() Scrubber {
	return func(s string) string { return reRFC3339.ReplaceAllString(s, "<RFC3339_DATE>") }
}

func ScrubEphemeralPorts() Scrubber {
	return func(s string) string { return reAddr.ReplaceAllString(s, "127.0.0.1:<PORT>") }
}

// Indexed GUID scrubber: preserves identity relationships (guid_1 == guid_1, guid_1 != guid_2)
func ScrubGUIDs() Scrubber {
	return func(s string) string {
		seen := map[string]string{}
		return reUUID.ReplaceAllStringFunc(s, func(m string) string {
			if tok, ok := seen[m]; ok {
				return tok
			}
			tok := fmt.Sprintf("<guid_%d>", len(seen)+1)
			seen[m] = tok
			return tok
		})
	}
}
```

With `go-approval-tests`:
```go
opts := approvals.Options().
	WithRegexScrubber(reRFC3339, "<RFC3339_DATE>").
	WithRegexScrubber(reAddr, "127.0.0.1:<PORT>")
approvals.VerifyString(t, rawHTTPResponse, opts)
```

### 3. Combinatorial Testing (`VerifyAllCombinations`)

Capture the entire Cartesian product of inputs in a single `.approved.txt` file. Ideal for CRUSH placement matrices, replication mode transitions, or S3 header validation:

```go
func TestVerifyReplicationTransitions(t *testing.T) {
	modes := []int{1, 2, 3, 4} // Chain, Splay, PrimarySplay, None
	cpuLoads := []float64{15.0, 50.0, 92.0}
	memLoads := []float64{20.0, 75.0, 95.0}

	var out strings.Builder
	for _, mode := range modes {
		for _, cpu := range cpuLoads {
			for _, mem := range memLoads {
				next, err := EvaluatePolymorphicTier(mode, cpu, mem)
				fmt.Fprintf(&out, "(mode=%d, cpu=%.1f, mem=%.1f) => next=%d, err=%v\n",
					mode, cpu, mem, next, err)
			}
		}
	}
	Verify(t, out.String())
}
```

Or using `go-approval-tests`:
```go
approvals.VerifyAllCombinationsFor3(
	t,
	"EvaluatePolymorphicTier",
	func(mode, cpu, mem any) string {
		next, err := EvaluatePolymorphicTier(mode.(int), cpu.(float64), mem.(float64))
		return fmt.Sprintf("next=%d err=%v", next, err)
	},
	[]any{1, 2, 3, 4},
	[]any{15.0, 50.0, 92.0},
	[]any{20.0, 75.0, 95.0},
)
```

### 4. Subtests (`t.Run`) & Multiple Approvals Per Test

In Go, calling `Verify` inside `t.Run("scenario", func(t *testing.T) { ... })` automatically uses `t.Name()` (e.g., `TestS3Tagging/empty_tag_set`), producing a distinct file `testdata/TestS3Tagging.empty_tag_set.approved.txt` per subtest without overwriting.

### 5. Storyboard Pattern (State Machine / Replication Progression)

```go
type Storyboard struct {
	buf   strings.Builder
	frame int
}

func NewStoryboard(title string) *Storyboard {
	var sb Storyboard
	fmt.Fprintf(&sb.buf, "=== %s ===\n", title)
	return &sb
}

func (s *Storyboard) AddFrame(label string, state any) *Storyboard {
	s.frame++
	fmt.Fprintf(&s.buf, "\n--- Frame %d: %s ---\n%v\n", s.frame, label, state)
	return s
}

func (s *Storyboard) String() string { return s.buf.String() }
```

---

## Git Setup

Add to `.gitignore`:
```gitignore
*.received.*
testdata/**/*.received.*
```

Commit all `testdata/**/*.approved.*` (or `testdata/**/*.golden`) files.

For cross-platform LF consistency, add to `.gitattributes`:
```gitattributes
*.approved.* text eol=lf
*.golden text eol=lf
```

---

## Reference Files (Deep Dives)

- [references/golang/api.md](references/golang/api.md) — `VerifyXML`, `VerifyHTTPResponse` (`httptest.ResponseRecorder`), `VerifyBytesHexDump` (wire protocols), `txtar` multi-file fixtures, and `MarkdownTable`
- [references/golang/scrubbers.md](references/golang/scrubbers.md) — Deterministic JSON map sorting, Go random map order traps, SHA-256 / UploadID / pointer-address indexed scrubbers, line scrubbers
- [references/golang/patterns.md](references/golang/patterns.md) — `log/slog` & `log.SetOutput` approval capture, pairwise combinations, inline raw-string approvals, and Go CI troubleshooting

