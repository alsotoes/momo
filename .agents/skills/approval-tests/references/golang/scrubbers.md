# Go Scrubbers & Determinism Traps

In Go, flaky approval tests across runs or machines almost always stem from five sources:
1. **Randomized `map` iteration order** (Go runtime intentionally randomizes map iteration starting seed).
2. **Timestamps** (`time.Now()`, `RFC3339Nano`, HTTP `Date` / `Last-Modified` headers, Unix nanos).
3. **Ephemeral network addresses** (`127.0.0.1:0` -> `127.0.0.1:43219`).
4. **Generated IDs / Hashes** (UUIDs, S3 `UploadId`, temporary file names `os.CreateTemp`).
5. **Memory pointer addresses & OS temp paths** (`0xc0000140a0`, `/tmp/...` vs `/var/folders/...`).

---

## 1. Composing Scrubbers in Go

```go
type Scrubber func(string) string

func CombineScrubbers(scrubbers ...Scrubber) Scrubber {
	return func(s string) string {
		for _, scrub := range scrubbers {
			s = scrub(s)
		}
		return s
	}
}
```

---

## 2. Regex & Indexed Scrubbers

Use **static replacement** when identity doesn't matter (e.g., `Date` header), and **indexed replacement** (`<id_1>`, `<id_2>`) when you need to prove two values in the output refer to the *same* entity:

```go
func CreateRegexScrubber(pattern, replacement string) Scrubber {
	re := regexp.MustCompile(pattern)
	return func(s string) string {
		return re.ReplaceAllString(s, replacement)
	}
}

func CreateIndexedRegexScrubber(pattern, prefix string) Scrubber {
	re := regexp.MustCompile(pattern)
	return func(s string) string {
		seen := make(map[string]string)
		return re.ReplaceAllStringFunc(s, func(match string) string {
			if tok, ok := seen[match]; ok {
				return tok
			}
			tok := fmt.Sprintf("<%s_%d>", prefix, len(seen)+1)
			seen[match] = tok
			return tok
		})
	}
}
```

### Ready-to-Use Go Scrubbers

```go
var (
	// RFC3339 / RFC3339Nano timestamps (e.g. 2026-11-27T11:20:00.123456789Z)
	ScrubRFC3339 = CreateIndexedRegexScrubber(
		`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`, "date")

	// HTTP RFC1123 Date headers (e.g. Fri, 27 Nov 2026 17:20:00 GMT)
	ScrubHTTPDate = CreateRegexScrubber(
		`[A-Z][a-z]{2}, \d{2} [A-Z][a-z]{2} \d{4} \d{2}:\d{2}:\d{2} GMT`, "<HTTP_DATE>")

	// 19-digit Unix Nanosecond timestamps (e.g. Momo handshake timestamps)
	ScrubUnixNano = CreateIndexedRegexScrubber(`\b1[7-9]\d{17}\b`, "unix_nano")

	// UUID v4 / S3 Request & Upload IDs
	ScrubUUIDs = CreateIndexedRegexScrubber(
		`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`, "guid")

	// Ephemeral loopback ports (127.0.0.1:54321 or [::1]:54321)
	ScrubLoopbackPorts = CreateIndexedRegexScrubber(
		`(?:127\.0\.0\.1|\[::1\]|localhost):\d{2,5}`, "addr")

	// Go pointer hex addresses (0xc000123456)
	ScrubPointerAddrs = CreateRegexScrubber(`0x[0-9a-f]{6,16}`, "<PTR>")
)
```

---

## 3. Scrubbing `t.TempDir()` and OS Paths

Always scrub `t.TempDir()` before verifying output that logs or serializes file paths:

```go
func ScrubTempDir(tempDir string) Scrubber {
	clean := filepath.ToSlash(tempDir)
	return func(s string) string {
		s = strings.ReplaceAll(filepath.ToSlash(s), clean, "<TEMP_DIR>")
		return strings.ReplaceAll(s, tempDir, "<TEMP_DIR>")
	}
}
```

---

## 4. Line Scrubber (Filtering Noisy Lines)

```go
func ScrubLinesContaining(substrings ...string) Scrubber {
	return func(s string) string {
		var kept []string
		for _, line := range strings.Split(s, "\n") {
			skip := false
			for _, sub := range substrings {
				if strings.Contains(line, sub) {
					skip = true
					break
				}
			}
			if !skip {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}
}
```

---

## 5. Defeating Go's Randomized Map Iteration Order

- **`encoding/json.Marshal` / `json.Encoder`**: Go's `encoding/json` automatically sorts `map[string]V` keys alphabetically! Therefore, `VerifyAsJSON(t, myMap)` is **always deterministic**.
- **`fmt.Sprintf("%v", myMap)`**: In Go 1.12+, `fmt` also sorts map keys when formatting `%v`, **except** if you iterate manually with `for k, v := range myMap` to build a custom string or XML payload.
- **Custom String / XML Builders**: Never range over a `map` directly when building output; extract keys into a slice, call `slices.Sort(keys)`, and iterate in sorted key order.
