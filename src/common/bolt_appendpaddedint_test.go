package common

import (
	"bytes"
	"syscall"
	"testing"
)

func TestWritePaddedInt(t *testing.T) {
	var buf [64]byte
	if err := WritePaddedInt(buf[:], 12345, 64); err != nil {
		t.Fatalf("WritePaddedInt failed: %v", err)
	}

	expected := make([]byte, 64)
	copy(expected, "12345")
	if !bytes.Equal(buf[:], expected) {
		t.Errorf("Expected %v, got %v", expected, buf[:])
	}
}

func TestWritePaddedInt_OverwritesFromStart(t *testing.T) {
	var buf [64]byte
	// Pre-fill to detect accidental append-at-end semantics.
	for i := range buf {
		buf[i] = 'x'
	}
	if err := WritePaddedInt(buf[:], 42, 4); err != nil {
		t.Fatalf("WritePaddedInt failed: %v", err)
	}
	expected := []byte{'4', '2', 0, 0}
	if !bytes.Equal(buf[:4], expected) {
		t.Errorf("Expected %v, got %v", expected, buf[:4])
	}
	// Content beyond the written field must remain untouched.
	for i := 4; i < len(buf); i++ {
		if buf[i] != 'x' {
			t.Errorf("buf[%d] modified beyond field width: got %q, want 'x'", i, buf[i])
		}
	}
}

func TestWritePaddedInt_Errors(t *testing.T) {
	if err := WritePaddedInt(make([]byte, 3), 12345, 64); err != syscall.EINVAL {
		t.Errorf("expected EINVAL for short dst, got %v", err)
	}
	if err := WritePaddedInt(make([]byte, 64), 123456789012345, 4); err != syscall.EINVAL {
		t.Errorf("expected EINVAL for oversized int, got %v", err)
	}
}
