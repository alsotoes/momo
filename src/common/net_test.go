package common

import (
	"net"
	"testing"
	"time"

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

// mockConn is a mock implementation of net.Conn for testing.
type mockConn struct {
	net.Conn
	readDeadlineSet  bool
	writeDeadlineSet bool
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	m.readDeadlineSet = true
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	m.writeDeadlineSet = true
	return nil
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func TestIdleTimeoutConn(t *testing.T) {
	mock := &mockConn{}
	idleConn := NewIdleTimeoutConn(mock, 30*time.Second)

	// Test Read
	buf := make([]byte, 10)
	_, _ = idleConn.Read(buf)
	if !mock.readDeadlineSet {
		t.Error("Expected SetReadDeadline to be called on Read")
	}

	// Test Write
	_, _ = idleConn.Write(buf)
	if !mock.writeDeadlineSet {
		t.Error("Expected SetWriteDeadline to be called on Write")
	}
}
