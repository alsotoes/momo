package server

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestEncodeDecodeFileMetadataList_ModTime(t *testing.T) {
	files := []common.FileMetadata{
		{Name: "a.txt", Hash: "hash1", Size: 10, RemotePath: "", ModTime: 1700000000123456789},
		{Name: "b.txt", Hash: "hash2", Size: 20, RemotePath: "docs", ModTime: 0},
		{Name: "c.txt", Hash: "hash3", Size: 30, RemotePath: "deep/nested", ModTime: 1700000000987654321},
	}

	encoded := EncodeFileMetadataList(files)
	decoded, err := DecodeFileMetadataList(encoded)
	if err != nil {
		t.Fatalf("DecodeFileMetadataList failed: %v", err)
	}
	if len(decoded) != len(files) {
		t.Fatalf("Expected %d entries, got %d", len(files), len(decoded))
	}
	for i := range files {
		if decoded[i] != files[i] {
			t.Errorf("Entry %d mismatch:\n got  %+v\n want %+v", i, decoded[i], files[i])
		}
	}
}

func TestDecodeFileMetadataList_RejectsOversizedCount(t *testing.T) {
	// A payload claiming far more entries than its length can contain must be rejected (Rule 32).
	bad := []byte{0, 0, 0, 200} // count=200 with no actual entries
	if _, err := DecodeFileMetadataList(bad); err == nil {
		t.Fatal("Expected error for oversized count, got nil")
	}
}
