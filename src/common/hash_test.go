package common

import (
	"bytes"
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

func TestHashReader(t *testing.T) {
	content := []byte("hello world")
	got, err := HashReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("HashReader failed: %v", err)
	}
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Errorf("HashReader mismatch: got %s, want %s", got, want)
	}
	if got != HashBytes(content) {
		t.Errorf("HashReader != HashBytes: %s vs %s", got, HashBytes(content))
	}
}

func TestHashReader_ErrorPropagated(t *testing.T) {
	_, err := HashReader(&errReader{err: os.ErrClosed})
	if err == nil {
		t.Error("expected error from failing reader")
	}
}

// errReader always fails reads with the given error.
type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

func TestHashFile_NonExistentFile(t *testing.T) {
	_, err := HashFile("non-existent-file.txt")
	if err == nil {
		t.Error("Expected an error when hashing a non-existent file, but got nil")
	}
}
