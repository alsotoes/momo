package server

import (
	"errors"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestDecodeFileMetadataList_CRLF(t *testing.T) {
	files := []common.FileMetadata{
		{
			Name:       "file1\r\n.txt",
			Hash:       "hash1",
			Size:       1024,
			RemotePath: "path1",
			ModTime:    1234567890,
		},
		{
			Name:       "file2",
			Hash:       "hash\r\n2",
			Size:       2048,
			RemotePath: "path2",
			ModTime:    1234567891,
		},
		{
			Name:       "file3",
			Hash:       "hash3",
			Size:       2048,
			RemotePath: "path\r\n3",
			ModTime:    1234567891,
		},
	}

	for i, f := range files {
		encoded := EncodeFileMetadataList([]common.FileMetadata{f})
		_, err := DecodeFileMetadataList(encoded)
		if err == nil {
			t.Fatalf("Expected DecodeFileMetadataList to fail for CRLF in entry %d", i)
		}
		if !errors.Is(err, syscall.EBADMSG) {
			t.Errorf("Expected EBADMSG error for entry %d, got: %v", i, err)
		}
	}
}
