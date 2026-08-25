package transport

import (
	"bytes"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func BenchmarkFormatListObjectsV2XML_AppendFormat(b *testing.B) {
	files := make([]common.FileMetadata, 1000)
	for i := range files {
		files[i] = common.FileMetadata{Name: "obj/key/file", Hash: "hash", Size: 1234, ModTime: int64(i) * 1e9}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := FormatListObjectsV2XML("mybucket", "", "", 1000, "", false, files); err != nil {
			b.Fatalf("FormatListObjectsV2XML: %v", err)
		}
	}
}

func BenchmarkFormatListObjectsV2XML_OldFormat(b *testing.B) {
	files := make([]common.FileMetadata, 1000)
	for i := range files {
		files[i] = common.FileMetadata{Name: "obj/key/file", Hash: "hash", Size: 1234, ModTime: int64(i) * 1e9}
	}
	var buf bytes.Buffer
	oldEmit := func(file common.FileMetadata, key string) {
		buf.Reset()
		buf.WriteString(`<Contents>`)
		buf.WriteString(`<Key>`)
		buf.WriteString(key)
		buf.WriteString(`</Key>`)
		buf.WriteString(`<LastModified>`)
		buf.WriteString(time.Unix(0, file.ModTime).UTC().Format("2006-01-02T15:04:05.000Z"))
		buf.WriteString(`</LastModified>`)
		buf.WriteString(`<ETag>"`)
		buf.WriteString(file.Hash)
		buf.WriteString(`"</ETag>`)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range files {
			oldEmit(f, f.Name)
		}
	}
}
