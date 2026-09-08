package transport

import (
	"bytes"
	"testing"
	"time"
)

// BenchmarkListPartsTime_AppendFormat measures the new allocation-free
// timestamp rendering used in handleListParts (time.AppendFormat into a
// stack-allocated [32]byte buffer, written to the XML buffer).
func BenchmarkListPartsTime_AppendFormat(b *testing.B) {
	var timeBuf [32]byte
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		tstr := time.Now().UTC().AppendFormat(timeBuf[:0], time.RFC3339)
		buf.WriteString(`<LastModified>`)
		buf.Write(tstr)
		buf.WriteString(`</LastModified>`)
	}
}

// BenchmarkListPartsTime_Format measures the previous time.Format
// implementation for comparison.
func BenchmarkListPartsTime_Format(b *testing.B) {
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		tstr := time.Now().UTC().Format(time.RFC3339)
		buf.WriteString(`<LastModified>`)
		buf.WriteString(tstr)
		buf.WriteString(`</LastModified>`)
	}
}
