package common

import (
	"net"
	"testing"

	"go.uber.org/goleak"
)

func TestDialSocket(t *testing.T) {
	defer goleak.VerifyNone(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	conn, err := DialSocket(addr)
	if err != nil {
		t.Errorf("DialSocket failed: %v", err)
	}
	if conn == nil {
		t.Error("Expected connection, got nil")
	} else {
		conn.Close()
	}

	_, err = DialSocket("invalid_address")
	if err == nil {
		t.Error("Expected error for invalid address, got nil")
	}
}
