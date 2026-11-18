package transport

import (
	"testing"
	"time"
)

// TestMultipartStatsSumsOpenUploads proves the scrape-time snapshot reports the
// open uploads and the bytes their spooled parts hold (issue #1369).
func TestMultipartStatsSumsOpenUploads(t *testing.T) {
	muUploads.Lock()
	beforeUploads, beforeBytes := len(uploads), int64(0)
	for _, up := range uploads {
		up.mu.Lock()
		for _, p := range up.parts {
			beforeBytes += p.size
		}
		up.mu.Unlock()
	}
	muUploads.Unlock()

	first := &multipartUpload{bucket: "b", key: "one", createdAt: time.Now()}
	first.parts = []multipartPart{{partNumber: 1, size: 100}, {partNumber: 2, size: 250}}
	second := &multipartUpload{bucket: "b", key: "two", createdAt: time.Now()}
	second.parts = []multipartPart{{partNumber: 1, size: 50}}

	muUploads.Lock()
	uploads["stats-one"] = first
	uploads["stats-two"] = second
	muUploads.Unlock()
	t.Cleanup(func() {
		muUploads.Lock()
		delete(uploads, "stats-one")
		delete(uploads, "stats-two")
		muUploads.Unlock()
	})

	active, bytes := MultipartStats()
	if want := beforeUploads + 2; active != want {
		t.Fatalf("active = %d, want %d", active, want)
	}
	if want := beforeBytes + 400; bytes != want {
		t.Fatalf("spill bytes = %d, want %d", bytes, want)
	}
}

// TestMultipartStatsEmpty proves the snapshot is safe and zero-valued with no
// uploads open.
func TestMultipartStatsEmpty(t *testing.T) {
	muUploads.Lock()
	saved := uploads
	uploads = make(map[string]*multipartUpload)
	muUploads.Unlock()
	t.Cleanup(func() {
		muUploads.Lock()
		uploads = saved
		muUploads.Unlock()
	})

	active, bytes := MultipartStats()
	if active != 0 || bytes != 0 {
		t.Fatalf("got (%d,%d), want (0,0)", active, bytes)
	}
}
