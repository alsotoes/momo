package common

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"go.uber.org/goleak"
)

type mockConn struct {
	net.Conn
	readDeadlineSet  bool
	writeDeadlineSet bool
	readError        error
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
	if m.readError != nil {
		return 0, m.readError
	}
	return len(b), nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func TestIdleTimeoutConn(t *testing.T) {
	defer goleak.VerifyNone(t)

	mc := &mockConn{}
	idleConn := NewIdleTimeoutConn(mc, 30*time.Second)

	// Test Read sets deadline
	idleConn.Read([]byte("test"))
	if !mc.readDeadlineSet {
		t.Error("Expected Read to set read deadline")
	}

	// Test Write sets deadline
	idleConn.Write([]byte("test"))
	if !mc.writeDeadlineSet {
		t.Error("Expected Write to set write deadline")
	}
}

func TestIdleTimeoutConn_ReadError(t *testing.T) {
	defer goleak.VerifyNone(t)

	mc := &mockConn{readError: net.ErrClosed}
	idleConn := NewIdleTimeoutConn(mc, 30*time.Second)

	// Test Read still sets deadline even if underlying read fails
	n, err := idleConn.Read([]byte("test"))
	if !mc.readDeadlineSet {
		t.Error("Expected Read to set read deadline before returning error")
	}
	if !errors.Is(err, syscall.ECONNABORTED) {
		t.Errorf("Expected err to wrap %v, got %v", syscall.ECONNABORTED, err)
	}
	if n != 0 {
		t.Errorf("Expected n to be 0, got %d", n)
	}
}

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
