// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// TestGetMetadata verifies that the getMetadata function correctly reads
// file metadata from a network connection.
func TestGetMetadata(t *testing.T) {
	// Arrange: Set up a network pipe to simulate a client-server connection without real networking.
	server, client := net.Pipe()

	// Arrange: Create a temporary file to get realistic test data (SHA-256 hash).
	tempFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	fileName := "test.txt"
	fileContent := "hello world"
	tempFile.Write([]byte(fileContent))
	tempFile.Close()

	fileHash, _ := common.HashFile(tempFile.Name())
	fileSize := len(fileContent)

	// Act: Start a goroutine to simulate the client sending metadata over the pipe.
	go func() {
		defer client.Close()

		// Send SHA-256 hash
		client.Write([]byte(fileHash))

		// Send file name, padded to the fixed buffer size
		fileNameBytes := make([]byte, common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		// Send file size, padded to the fixed buffer size
		fileSizeBytes := make([]byte, common.FileInfoLength)
		copy(fileSizeBytes, strconv.Itoa(fileSize))
		client.Write(fileSizeBytes)
	}()

	// Act: Call the function under test, which reads from the server side of the pipe.
	metadata, err := getMetadata(server)

	if err != nil {
		t.Fatalf("getMetadata failed: %v", err)
	}

	// Assert: Verify that the received metadata matches the expected values.
	if metadata.Name != fileName {
		t.Errorf("Expected file name '%s', got '%s'", fileName, metadata.Name)
	}
	if metadata.Hash != fileHash {
		t.Errorf("Expected file hash '%s', got '%s'", fileHash, metadata.Hash)
	}
	if metadata.Size != int64(fileSize) {
		t.Errorf("Expected file size %d, got %d", fileSize, metadata.Size)
	}
}

func TestGetMetadataDotDot(t *testing.T) {
	// Arrange: Set up a network pipe to simulate a client-server connection without real networking.
	server, client := net.Pipe()

	fileName := ".."
	fileHash := "de614ea622e0963faf12594c1c59937dcb6fc223c81b3a451ee2561fc44e22a2"
	fileSize := 10

	// Act: Start a goroutine to simulate the client sending metadata over the pipe.
	go func() {
		defer client.Close()

		// Send SHA-256 hash
		client.Write([]byte(fileHash))

		// Send file name, padded to the fixed buffer size
		fileNameBytes := make([]byte, common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		// Send file size, padded to the fixed buffer size
		fileSizeBytes := make([]byte, common.FileInfoLength)
		copy(fileSizeBytes, strconv.Itoa(fileSize))
		client.Write(fileSizeBytes)
	}()

	// Act: Call the function under test, which reads from the server side of the pipe.
	_, err := getMetadata(server)

	if err == nil {
		t.Fatalf("getMetadata should have failed for filename '%s'", fileName)
	}
}

func TestGetMetadataInvalidNames(t *testing.T) {
	invalidNames := []string{
		"C:\\Windows\\System32\\cmd.exe",
		"foo\\bar.txt",
	}

	for _, fileName := range invalidNames {
		t.Run(fileName, func(t *testing.T) {
			server, client := net.Pipe()
			fileHash := "de614ea622e0963faf12594c1c59937dcb6fc223c81b3a451ee2561fc44e22a2"
			fileSize := 10

			go func() {
				defer client.Close()

				client.Write([]byte(fileHash))

				fileNameBytes := make([]byte, common.FileInfoLength)
				copy(fileNameBytes, fileName)
				client.Write(fileNameBytes)

				fileSizeBytes := make([]byte, common.FileInfoLength)
				copy(fileSizeBytes, strconv.Itoa(fileSize))
				client.Write(fileSizeBytes)
			}()

			_, err := getMetadata(server)

			if err == nil {
				t.Fatalf("getMetadata should have failed for filename '%s'", fileName)
			}
		})
	}
}

func TestGetFileTraversal(t *testing.T) {
	server, client := net.Pipe()

	tempDir, err := os.MkdirTemp("", "test-getfile")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	store, err := storage.NewCASStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create CAS store: %v", err)
	}
	defer store.Close()

	traversalFileName := "../traversal.txt"
	fileContent := "dangerous content"

	hash := sha256.New()
	io.WriteString(hash, fileContent)
	fileHash := hex.EncodeToString(hash.Sum(nil))
	fileSize := int64(len(fileContent))

	go func() {
		defer client.Close()
		client.Write([]byte(fileContent))
	}()

	// In a real scenario, the server would call getFile after getMetadata.
	// Since getMetadata now sanitizes the filename, we pass the sanitized name to getFile
	// to simulate the real behavior.

	comm := transport.NewMomoTCPCommunicator(server)
	sanitizedFileName := filepath.Base(traversalFileName)
	getFile(comm, store, sanitizedFileName, fileHash, fileSize)

	// The file should be created in storageDir/blobs/..., NOT in tempDir/traversal.txt
	traversalFilePath := filepath.Join(tempDir, "traversal.txt")
	if _, err := os.Stat(traversalFilePath); err == nil {
		t.Errorf("Vulnerability still exists: File created outside storage directory at %s", traversalFilePath)
	}

	safeFilePath, err := store.GetBlobPath(sanitizedFileName)
	if err != nil {
		t.Fatalf("Failed to get blob path: %v", err)
	}
	if _, err := os.Stat(safeFilePath); os.IsNotExist(err) {
		t.Errorf("Expected file to be created at %s, but it was not", safeFilePath)
	}
}

func TestParsePaddedIntFast(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int64
		err      error
	}{
		{
			name:     "positive number",
			input:    []byte("12345\x00\x00"),
			expected: 12345,
			err:      nil,
		},
		{
			name:     "negative number",
			input:    []byte("-12345\x00"),
			expected: -12345,
			err:      nil,
		},
		{
			name:     "math.MaxInt64",
			input:    []byte("9223372036854775807\x00"),
			expected: math.MaxInt64,
			err:      nil,
		},
		{
			name:     "math.MinInt64",
			input:    []byte("-9223372036854775808\x00"),
			expected: math.MinInt64,
			err:      nil,
		},
		{
			name:     "overflow math.MaxInt64 + 1",
			input:    []byte("9223372036854775808\x00"),
			expected: 0,
			err:      strconv.ErrRange,
		},
		{
			name:     "underflow math.MinInt64 - 1",
			input:    []byte("-9223372036854775809\x00"),
			expected: 0,
			err:      strconv.ErrRange,
		},
		{
			name:     "invalid characters",
			input:    []byte("12a45\x00"),
			expected: 0,
			err:      strconv.ErrSyntax,
		},
		{
			name:     "empty string",
			input:    []byte("\x00"),
			expected: 0,
			err:      strconv.ErrSyntax,
		},
		{
			name:     "just minus sign",
			input:    []byte("-\x00"),
			expected: 0,
			err:      strconv.ErrSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePaddedIntFast(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
			if err != tt.err {
				t.Errorf("expected error %v, got %v", tt.err, err)
			}
		})
	}
}

func FuzzParsePaddedIntFast(f *testing.F) {
	f.Add([]byte("12345"))
	f.Add([]byte("12345\x00\x00"))
	f.Add([]byte("-123"))
	f.Add([]byte("+456"))
	f.Add([]byte("9223372036854775807")) // MaxInt64
	f.Add([]byte("-9223372036854775808")) // MinInt64
	f.Add([]byte("9223372036854775808")) // Overflow
	f.Add([]byte("abc"))
	f.Add([]byte(""))
	f.Add([]byte("\x00\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parsePaddedIntFast(data)
	})
}

func FuzzGetMetadata(f *testing.F) {
	// Seed data: a valid 192-byte metadata packet
	valid := make([]byte, 192)
	copy(valid[:64], bytes.Repeat([]byte("a"), 64)) // Hash
	copy(valid[64:128], "test.txt") // Name
	copy(valid[128:], "12345") // Size
	f.Add(valid)
	
	f.Add(make([]byte, 192)) // All zeros
	f.Add(make([]byte, 10)) // Too short

	f.Fuzz(func(t *testing.T, data []byte) {
		reader := bytes.NewReader(data)
		_, _ = getMetadata(reader)
	})
}
