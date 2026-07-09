package common

import (
	"bytes"
	"testing"
)

func TestAppendPaddedInt(t *testing.T) {
	var buf [64]byte
	AppendPaddedInt(buf[:], 12345, 64)

	expected := make([]byte, 64)
	copy(expected, "12345")
	if !bytes.Equal(buf[:], expected) {
		t.Errorf("Expected %v, got %v", expected, buf[:])
	}
}
