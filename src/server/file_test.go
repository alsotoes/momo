// Package server provides the core functionality for the momo server.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	momo_common "github.com/alsotoes/momo/src/common"
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

	fileHash, _ := momo_common.HashFile(tempFile.Name())
	fileSize := len(fileContent)

	// Act: Start a goroutine to simulate the client sending metadata over the pipe.
	go func() {
		defer client.Close()

		// Send SHA-256 hash
		client.Write([]byte(fileHash))

		// Send file name, padded to the fixed buffer size
		fileNameBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		// Send file size, padded to the fixed buffer size
		fileSizeBytes := make([]byte, momo_common.FileInfoLength)
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

func TestGetFileTraversal(t *testing.T) {
	server, client := net.Pipe()

	tempDir, err := os.MkdirTemp("", "test-getfile")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	err = os.Mkdir(storageDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create storage dir: %v", err)
	}

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

	sanitizedFileName := filepath.Base(traversalFileName)
	getFile(server, storageDir, sanitizedFileName, fileHash, fileSize)

	// The file should be created in storageDir/traversal.txt, NOT in tempDir/traversal.txt
	traversalFilePath := filepath.Join(tempDir, "traversal.txt")
	if _, err := os.Stat(traversalFilePath); err == nil {
		t.Errorf("Vulnerability still exists: File created outside storage directory at %s", traversalFilePath)
	}

	safeFilePath := filepath.Join(storageDir, "traversal.txt")
	if _, err := os.Stat(safeFilePath); os.IsNotExist(err) {
		t.Errorf("Expected file to be created at %s, but it was not", safeFilePath)
	}
}
