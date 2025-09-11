package common

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestHashFile(t *testing.T) {
	// Create a temporary file with known content
	content := []byte("hello world")
	tmpfile, err := ioutil.TempFile("", "test.txt")
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

	// The expected MD5 hash of "hello world"
	expectedHash := "5eb63bbbe01eeed093cb22bb8f5acdc3"

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
