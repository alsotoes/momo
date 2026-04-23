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

type mockConn struct {
	readDeadline  time.Time
	writeDeadline time.Time
	readCalled    bool
	writeCalled   bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	m.readCalled = true
	return len(b), nil
}
func (m *mockConn) Write(b []byte) (n int, err error) {
	m.writeCalled = true
	return len(b), nil
}
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { m.readDeadline = t; return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { m.writeDeadline = t; return nil }

func TestIdleTimeoutConn_Read(t *testing.T) {
	defer goleak.VerifyNone(t)
	mock := &mockConn{}
	timeout := 5 * time.Second
	c := NewIdleTimeoutConn(mock, timeout)

	b := make([]byte, 10)
	now := time.Now()
	_, err := c.Read(b)
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}

	if !mock.readCalled {
		t.Error("Expected underlying Read to be called")
	}

	if mock.readDeadline.Before(now.Add(timeout)) {
		t.Errorf("Expected read deadline to be at least %v, got %v", now.Add(timeout), mock.readDeadline)
	}
}

func TestIdleTimeoutConn_Write(t *testing.T) {
	defer goleak.VerifyNone(t)
	mock := &mockConn{}
	timeout := 5 * time.Second
	c := NewIdleTimeoutConn(mock, timeout)

	b := []byte("hello")
	now := time.Now()
	_, err := c.Write(b)
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	if !mock.writeCalled {
		t.Error("Expected underlying Write to be called")
	}

	if mock.writeDeadline.Before(now.Add(timeout)) {
		t.Errorf("Expected write deadline to be at least %v, got %v", now.Add(timeout), mock.writeDeadline)
	}
}
