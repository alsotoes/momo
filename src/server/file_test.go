// Package server provides the core functionality for the momo server.
package server

import (
	"net"
	"os"
	"strconv"
	"testing"

	momo_common "github.com/alsotoes/momo/src/common"
)

// TestGetMetadata verifies that the getMetadata function correctly reads
// file metadata from a network connection.
func TestGetMetadata(t *testing.T) {
	// Arrange: Set up a network pipe to simulate a client-server connection without real networking.
	server, client := net.Pipe()

	// Arrange: Create a temporary file to get realistic test data (MD5 hash).
	tempFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	fileName := "test.txt"
	fileContent := "hello world"
	tempFile.Write([]byte(fileContent))
	tempFile.Close()

	fileMD5, _ := momo_common.HashFile(tempFile.Name())
	fileSize := len(fileContent)

	// Act: Start a goroutine to simulate the client sending metadata over the pipe.
	go func() {
		defer client.Close()

		// Send MD5 hash
		client.Write([]byte(fileMD5))

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
	metadata := getMetadata(server)

	// Assert: Verify that the received metadata matches the expected values.
	if metadata.Name != fileName {
		t.Errorf("Expected file name '%s', got '%s'", fileName, metadata.Name)
	}
	if metadata.MD5 != fileMD5 {
		t.Errorf("Expected file MD5 '%s', got '%s'", fileMD5, metadata.MD5)
	}
	if metadata.Size != int64(fileSize) {
		t.Errorf("Expected file size %d, got %d", fileSize, metadata.Size)
	}
}
