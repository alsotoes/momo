package common

import (
	"os"
	"testing"
)

func TestHashFile(t *testing.T) {
	// Create a temporary file with known content
	content := []byte("hello world")
	tmpfile, err := os.CreateTemp("", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// The expected SHA-256 hash of "hello world"
	expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	actualHash, err := HashFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}

	if actualHash != expectedHash {
		t.Errorf("Expected hash %s, but got %s", expectedHash, actualHash)
	}
}

func TestHashFile_NonExistentFile(t *testing.T) {
	_, err := HashFile("non-existent-file.txt")
	if err == nil {
		t.Error("Expected an error when hashing a non-existent file, but got nil")
	}
}
