package server

import (
	"bytes"
	"strings"
	"testing"
)

// TestMultipartGaugesAreEmitted proves both multipart families appear on every
// scrape, even at zero, so a dashboard can tell "none" from "not reported"
// (issue #1369).
func TestMultipartGaugesAreEmitted(t *testing.T) {
	var buf bytes.Buffer
	writeMultipartGauges(&buf)
	out := buf.String()

	for _, want := range []string{
		"# TYPE momo_multipart_uploads_active gauge",
		"momo_multipart_uploads_active 0",
		"# TYPE momo_multipart_spill_bytes gauge",
		"momo_multipart_spill_bytes 0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrape missing %q:\n%s", want, out)
		}
	}
}
