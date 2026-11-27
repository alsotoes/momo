# Go ApprovalTests API & Specialized Verifiers

Beyond `Verify` and `VerifyAsJSON`, Go systems and web services benefit from specialized approval verifiers for XML (e.g., S3 REST APIs), HTTP handlers (`httptest.ResponseRecorder`), binary wire protocols (`encoding/hex`), and multi-file test archives (`txtar`).

---

## 1. `VerifyHTTPResponse` (`net/http/httptest`)

Captures the HTTP status line, sorted deterministic headers, and body in one human-scannable HTTP wireframe snapshot:

```go
func FormatHTTPResponse(rec *httptest.ResponseRecorder) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\n", rec.Code, http.StatusText(rec.Code))

	keys := make([]string, 0, len(rec.Header()))
	for k := range rec.Header() {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		for _, v := range rec.Header()[k] {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("\n")
	b.Write(rec.Body.Bytes())
	return b.String()
}

func VerifyHTTPResponse(t *testing.T, rec *httptest.ResponseRecorder, scrubbers ...Scrubber) {
	t.Helper()
	Verify(t, FormatHTTPResponse(rec), scrubbers...)
}
```

**Why sorting headers matters:** Go's `http.Header` is a `map[string][]string`. Iterating over a Go map without `slices.Sort(keys)` produces non-deterministic header ordering on every test run!

---

## 2. `VerifyXML` (S3 Responses & Structured XML)

Pretty-prints raw XML tokens so single-line XML responses produce readable, line-by-line diffs:

```go
func FormatXML(t *testing.T, rawXML string) string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(rawXML))
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("FormatXML decode: %v", err)
		}
		if err := enc.EncodeToken(tok); err != nil {
			t.Fatalf("FormatXML encode: %v", err)
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("FormatXML flush: %v", err)
	}
	return buf.String()
}

func VerifyXML(t *testing.T, rawXML string, scrubbers ...Scrubber) {
	t.Helper()
	Verify(t, FormatXML(t, rawXML), scrubbers...)
}
```

---

## 3. `VerifyBytesHexDump` (Binary Wire Protocols & Padded Handshakes)

For binary TCP/QUIC frames or fixed-width padded handshakes (e.g., Momo's 19-byte timestamp + 32-byte MD5 + 64-byte filename), `hex.Dump(payload)` produces the classic 16-byte-per-line hex + ASCII layout:

```go
func VerifyBytesHexDump(t *testing.T, data []byte, scrubbers ...Scrubber) {
	t.Helper()
	Verify(t, hex.Dump(data), scrubbers...)
}
```

Example `.approved.txt` output:
```text
00000000  31 37 30 30 30 30 30 30  30 30 30 30 30 30 30 30  |1700000000000000|
00000010  30 30 30 31 6f 62 6a 2e  62 69 6e 00 00 00 00 00  |0001obj.bin.....|
```

---

## 4. `VerifyAll` & `MarkdownTable`

### `VerifyAll` (Labeled Slice Formatter)

```go
func VerifyAll[T any](t *testing.T, header string, items []T, format func(T) string, scrubbers ...Scrubber) {
	t.Helper()
	var b strings.Builder
	if header != "" {
		fmt.Fprintf(&b, "%s\n%s\n", header, strings.Repeat("=", len(header)))
	}
	for i, item := range items {
		fmt.Fprintf(&b, "[%d] %s\n", i, format(item))
	}
	Verify(t, b.String(), scrubbers...)
}
```

### `MarkdownTable` (Scannable Columnar Output)

```go
type MarkdownTable struct {
	headers []string
	rows    [][]string
}

func NewMarkdownTable(headers ...string) *MarkdownTable {
	return &MarkdownTable{headers: headers}
}

func (m *MarkdownTable) AddRow(cols ...any) *MarkdownTable {
	row := make([]string, len(cols))
	for i, c := range cols {
		row[i] = fmt.Sprint(c)
	}
	m.rows = append(m.rows, row)
	return m
}

func (m *MarkdownTable) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "| %s |\n", strings.Join(m.headers, " | "))
	sep := make([]string, len(m.headers))
	for i := range sep {
		sep[i] = "---"
	}
	fmt.Fprintf(&b, "| %s |\n", strings.Join(sep, " | "))
	for _, r := range m.rows {
		fmt.Fprintf(&b, "| %s |\n", strings.Join(r, " | "))
	}
	return b.String()
}
```

---

## 5. Multi-File Fixtures with `txtar` (Go Compiler / Tooling Pattern)

The Go standard toolchain uses `txtar` (text archive format) in `testdata/*.txtar` to bundle input files and expected approved output in a single file:

```text
-- input.conf --
[global]
replication = 2
-- approved.txt --
Mode: ReplicationSplay (2)
```

This matches the `bdd-with-approvals` "Approved Fixtures" pattern natively in Go.
