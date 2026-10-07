package server

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
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
		// S3Headers (a map) is intentionally not part of the wire framing, so
		// compare with DeepEqual rather than struct equality.
		if !reflect.DeepEqual(decoded[i], files[i]) {
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

// TestEncodeFileMetadataList_SkipsOversizedEntries verifies that entries whose
// name/hash exceed FileInfoLength or whose remote path exceeds MaxPathLength
// are skipped during encoding, so the output is always decodable by peers
// (fix #665).
func TestEncodeFileMetadataList_SkipsOversizedEntries(t *testing.T) {
	longName := strings.Repeat("n", common.FileInfoLength+1)
	longHash := strings.Repeat("h", common.FileInfoLength+1)
	longPath := strings.Repeat("p", common.MaxPathLength+1)

	files := []common.FileMetadata{
		{Name: "ok.txt", Hash: "hash", Size: 10, RemotePath: "", ModTime: 1},
		{Name: longName, Hash: "hash", Size: 10, RemotePath: "", ModTime: 1},
		{Name: "ok2.txt", Hash: longHash, Size: 10, RemotePath: "", ModTime: 1},
		{Name: "ok3.txt", Hash: "hash", Size: 10, RemotePath: longPath, ModTime: 1},
	}

	encoded := EncodeFileMetadataList(files)
	decoded, err := DecodeFileMetadataList(encoded)
	if err != nil {
		t.Fatalf("DecodeFileMetadataList failed on encoded output: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("Expected 1 surviving entry (only the fully-valid one), got %d", len(decoded))
	}
	if decoded[0].Name != "ok.txt" {
		t.Errorf("Expected surviving entry 'ok.txt', got %q", decoded[0].Name)
	}
}

// TestEncodeFileMetadataList_BoundarySizes verifies entries at exactly the
// length limits are kept (not skipped) — the decoder accepts them.
func TestEncodeFileMetadataList_BoundarySizes(t *testing.T) {
	files := []common.FileMetadata{
		{Name: strings.Repeat("n", common.FileInfoLength), Hash: "hash", Size: 10, RemotePath: "", ModTime: 1},
		{Name: "x.txt", Hash: strings.Repeat("h", common.FileInfoLength), Size: 10, RemotePath: strings.Repeat("p", common.MaxPathLength), ModTime: 1},
	}
	encoded := EncodeFileMetadataList(files)
	decoded, err := DecodeFileMetadataList(encoded)
	if err != nil {
		t.Fatalf("DecodeFileMetadataList failed on boundary-length entries: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("Expected 2 boundary entries to survive, got %d", len(decoded))
	}
}

// TestMergeFileMetadataLists_KeepsAliasNames verifies that files sharing a
// content hash but having different names (dedup aliases) are all kept, while
// identical name+hash entries from different lists are collapsed (fix #664).
func TestMergeFileMetadataLists_KeepsAliasNames(t *testing.T) {
	listA := []common.FileMetadata{
		{Name: "photo.jpg", Hash: "hash-abc", Size: 100, RemotePath: "", ModTime: 1},
	}
	listB := []common.FileMetadata{
		// Same content hash, different name: legitimate alias, must be kept.
		{Name: "IMG_001.jpg", Hash: "hash-abc", Size: 100, RemotePath: "", ModTime: 1},
		// Exact duplicate of an entry in listA: must be collapsed.
		{Name: "photo.jpg", Hash: "hash-abc", Size: 100, RemotePath: "", ModTime: 1},
		{Name: "doc.txt", Hash: "hash-def", Size: 200, RemotePath: "", ModTime: 2},
	}

	merged := MergeFileMetadataLists(listA, listB)
	if len(merged) != 3 {
		t.Fatalf("Expected 3 merged entries (2 aliases + 1 unique), got %d: %+v", len(merged), merged)
	}

	seen := make(map[string]bool)
	for _, f := range merged {
		key := f.Name + "|" + f.Hash
		if seen[key] {
			t.Errorf("Duplicate name+hash entry in merged output: %+v", f)
		}
		seen[key] = true
	}

	for _, want := range []common.FileMetadata{
		{Name: "photo.jpg", Hash: "hash-abc", Size: 100, RemotePath: "", ModTime: 1},
		{Name: "IMG_001.jpg", Hash: "hash-abc", Size: 100, RemotePath: "", ModTime: 1},
		{Name: "doc.txt", Hash: "hash-def", Size: 200, RemotePath: "", ModTime: 2},
	} {
		key := want.Name + "|" + want.Hash
		if !seen[key] {
			t.Errorf("Merged output missing expected entry %q", key)
		}
	}
}

func TestQueryHandler_PathTraversalRejection(t *testing.T) {
	h := NewStorageQueryHandler(nil)

	traversalNames := []string{
		"/etc/passwd",
		"/root/.ssh/id_rsa",
		"../foo",
		"foo/../../bar",
		"..",
		"dir\\file.txt",
	}

	for _, name := range traversalNames {
		t.Run("handleGet_"+name, func(t *testing.T) {
			_, err := h.handleGet([]byte(name))
			if err == nil {
				t.Fatalf("expected error for handleGet(%q), got nil", name)
			}
			if !errors.Is(err, syscall.EBADMSG) {
				t.Errorf("expected syscall.EBADMSG for handleGet(%q), got: %v", name, err)
			}
		})

		t.Run("handleDelete_"+name, func(t *testing.T) {
			_, err := h.handleDelete([]byte(name))
			if err == nil {
				t.Fatalf("expected error for handleDelete(%q), got nil", name)
			}
			if !errors.Is(err, syscall.EBADMSG) {
				t.Errorf("expected syscall.EBADMSG for handleDelete(%q), got: %v", name, err)
			}
		})
	}

	t.Run("handleGet_empty", func(t *testing.T) {
		_, err := h.handleGet([]byte(""))
		if err == nil || !errors.Is(err, syscall.EINVAL) {
			t.Errorf("expected syscall.EINVAL for empty handleGet, got: %v", err)
		}
	})

	t.Run("handleDelete_empty", func(t *testing.T) {
		_, err := h.handleDelete([]byte(""))
		if err == nil || !errors.Is(err, syscall.EINVAL) {
			t.Errorf("expected syscall.EINVAL for empty handleDelete, got: %v", err)
		}
	})

	t.Run("handleHas_empty", func(t *testing.T) {
		_, err := h.handleHas([]byte(""))
		if err == nil || !errors.Is(err, syscall.EINVAL) {
			t.Errorf("expected syscall.EINVAL for empty handleHas, got: %v", err)
		}
	})

	t.Run("DecodeFileMetadataList_traversal", func(t *testing.T) {
		for _, name := range traversalNames {
			badList := []common.FileMetadata{
				{Name: name, Hash: "validhash", Size: 10},
			}
			encoded := EncodeFileMetadataList(badList)
			_, err := DecodeFileMetadataList(encoded)
			if err == nil {
				t.Fatalf("expected error decoding list with traversal name %q, got nil", name)
			}
			if !errors.Is(err, syscall.EBADMSG) {
				t.Errorf("expected syscall.EBADMSG decoding traversal name %q, got: %v", name, err)
			}
		}
	})

	t.Run("handleGet_oversized", func(t *testing.T) {
		oversized := strings.Repeat("a", common.FileInfoLength+1)
		_, err := h.handleGet([]byte(oversized))
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("expected syscall.EBADMSG for oversized handleGet, got: %v", err)
		}
	})

	t.Run("handleDelete_oversized", func(t *testing.T) {
		oversized := strings.Repeat("a", common.FileInfoLength+1)
		_, err := h.handleDelete([]byte(oversized))
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("expected syscall.EBADMSG for oversized handleDelete, got: %v", err)
		}
	})

	t.Run("handleHas_oversized", func(t *testing.T) {
		oversized := strings.Repeat("a", common.FileInfoLength+1)
		_, err := h.handleHas([]byte(oversized))
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("expected syscall.EBADMSG for oversized handleHas, got: %v", err)
		}
	})

	t.Run("handleGet_crlf", func(t *testing.T) {
		_, err := h.handleGet([]byte("file\r\n.txt"))
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("expected syscall.EBADMSG for crlf handleGet, got: %v", err)
		}
	})

	t.Run("handleDelete_crlf", func(t *testing.T) {
		_, err := h.handleDelete([]byte("file\n.txt"))
		if err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("expected syscall.EBADMSG for crlf handleDelete, got: %v", err)
		}
	})

	t.Run("handleGet_and_handleDelete_valid", func(t *testing.T) {
		hValid := NewStorageQueryHandler(&queryMockStore{})
		res, err := hValid.handleGet([]byte("valid/path.txt"))
		if err != nil {
			t.Fatalf("unexpected error for valid handleGet: %v", err)
		}
		if len(res) == 0 {
			t.Errorf("expected non-empty result for valid handleGet")
		}

		resDel, err := hValid.handleDelete([]byte("valid/path.txt"))
		if err != nil {
			t.Fatalf("unexpected error for valid handleDelete: %v", err)
		}
		if len(resDel) == 0 || resDel[0] != 1 {
			t.Errorf("expected success byte for valid handleDelete")
		}
	})
}

type queryMockStore struct {
	storage.Store
}

func (m *queryMockStore) GetMeta(name string) (common.FileMetadata, error) {
	return common.FileMetadata{Name: name, Hash: "hash", Size: 100}, nil
}

func (m *queryMockStore) Delete(name string) error {
	return nil
}
