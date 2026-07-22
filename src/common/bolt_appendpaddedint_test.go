package common

import (
	"bytes"
	"testing"
)

func TestAppendPaddedInt(t *testing.T) {
	var buf [64]byte
	if err := AppendPaddedInt(buf[:], 12345, 64); err != nil {
		t.Fatalf("AppendPaddedInt failed: %v", err)
	}

	expected := make([]byte, 64)
	copy(expected, "12345")
	if !bytes.Equal(buf[:], expected) {
		t.Errorf("Expected %v, got %v", expected, buf[:])
	}
}
