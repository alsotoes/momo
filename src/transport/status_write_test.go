package transport

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
)

type failWriter struct{ err error }

func (w failWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestWriteStatusByte(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStatusByte(&buf, '1'); err != nil {
		t.Fatalf("writeStatusByte failed on healthy writer: %v", err)
	}
	if buf.Len() != 1 || buf.Bytes()[0] != '1' {
		t.Fatalf("expected single status byte '1', got %v", buf.Bytes())
	}

	err := writeStatusByte(failWriter{err: io.ErrClosedPipe}, '0')
	if err == nil {
		t.Fatal("expected error when the underlying write fails")
	}
	if !errors.Is(err, syscall.EIO) {
		t.Errorf("expected wrapped syscall.EIO, got %v", err)
	}
}
