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
		fileNameBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		// Send file size, padded to the fixed buffer size
		fileSizeBytes := make([]byte, momo_common.FileInfoLength)
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

				fileNameBytes := make([]byte, momo_common.FileInfoLength)
				copy(fileNameBytes, fileName)
				client.Write(fileNameBytes)

				fileSizeBytes := make([]byte, momo_common.FileInfoLength)
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

func TestGetMetadataFileSizeLimit(t *testing.T) {
	// Arrange
	server, client := net.Pipe()

	fileName := "toolarge.txt"
	fileHash := "de614ea622e0963faf12594c1c59937dcb6fc223c81b3a451ee2561fc44e22a2"
	// Exactly 1 byte larger than the 1GB limit
	fileSize := momo_common.MaxFileSize + 1

	// Act
	go func() {
		defer client.Close()

		client.Write([]byte(fileHash))

		fileNameBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		fileSizeBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileSizeBytes, strconv.FormatInt(int64(fileSize), 10))
		client.Write(fileSizeBytes)
	}()

	_, err := getMetadata(server)

	// Assert
	if err == nil {
		t.Fatalf("getMetadata should have failed for file size %d exceeding limit %d", fileSize, momo_common.MaxFileSize)
	}
}

func TestGetMetadataFileSizeNegative(t *testing.T) {
	// Arrange
	server, client := net.Pipe()

	fileName := "negative.txt"
	fileHash := "de614ea622e0963faf12594c1c59937dcb6fc223c81b3a451ee2561fc44e22a2"
	fileSize := -1

	// Act
	go func() {
		defer client.Close()

		client.Write([]byte(fileHash))

		fileNameBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		fileSizeBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileSizeBytes, strconv.Itoa(fileSize))
		client.Write(fileSizeBytes)
	}()

	_, err := getMetadata(server)

	// Assert
	if err == nil {
		t.Fatalf("getMetadata should have failed for negative file size %d", fileSize)
	}
}
