package momo

import (
	"net"
	"os"
	"strconv"
	"testing"

	momo_common "github.com/alsotoes/momo/src/common"
)

func TestGetMetadata(t *testing.T) {
	// Create a mock server and client
	server, client := net.Pipe()

	// Create a temporary file to hash
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

	// Write the metadata to the client connection
	go func() {
		defer client.Close()

		// Write the MD5 hash
		client.Write([]byte(fileMD5))

		// Write the file name
		fileNameBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileNameBytes, fileName)
		client.Write(fileNameBytes)

		// Write the file size
		fileSizeBytes := make([]byte, momo_common.FileInfoLength)
		copy(fileSizeBytes, strconv.Itoa(fileSize))
		client.Write(fileSizeBytes)
	}()

	// Read the metadata from the server connection
	metadata := getMetadata(server)

	// Check the metadata
	if metadata.Name != fileName {
		t.Errorf("Expected file name %s, got %s", fileName, metadata.Name)
	}
	if metadata.MD5 != fileMD5 {
		t.Errorf("Expected file MD5 %s, got %s", fileMD5, metadata.MD5)
	}
	if metadata.Size != int64(fileSize) {
		t.Errorf("Expected file size %d, got %d", fileSize, metadata.Size)
	}
}
