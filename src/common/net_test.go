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
	net.Conn
	lastReadDeadline  time.Time
	lastWriteDeadline time.Time
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	m.lastReadDeadline = t
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	m.lastWriteDeadline = t
	return nil
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Close() error {
	return nil
}

func TestIdleTimeoutConn_Read(t *testing.T) {
	mock := &mockConn{}
	timeout := 5 * time.Second
	itc := NewIdleTimeoutConn(mock, timeout)

	buffer := make([]byte, 10)
	_, err := itc.Read(buffer)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if mock.lastReadDeadline.IsZero() {
		t.Error("SetReadDeadline was not called")
	}

	expected := time.Now().Add(timeout)
	if mock.lastReadDeadline.Before(expected.Add(-1*time.Second)) || mock.lastReadDeadline.After(expected.Add(1*time.Second)) {
		t.Errorf("Read deadline not set correctly. Expected near %v, got %v", expected, mock.lastReadDeadline)
	}
}

func TestIdleTimeoutConn_Write(t *testing.T) {
	mock := &mockConn{}
	timeout := 5 * time.Second
	itc := NewIdleTimeoutConn(mock, timeout)

	data := []byte("hello")
	_, err := itc.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if mock.lastWriteDeadline.IsZero() {
		t.Error("SetWriteDeadline was not called")
	}

	expected := time.Now().Add(timeout)
	if mock.lastWriteDeadline.Before(expected.Add(-1*time.Second)) || mock.lastWriteDeadline.After(expected.Add(1*time.Second)) {
		t.Errorf("Write deadline not set correctly. Expected near %v, got %v", expected, mock.lastWriteDeadline)
	}
}

func TestIdleTimeoutConn_Integration(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	timeout := 1 * time.Second
	itc := NewIdleTimeoutConn(c1, timeout)

	msg := "ping"
	done := make(chan struct{})
	go func() {
		itc.Write([]byte(msg))
		close(done)
	}()

	buf := make([]byte, len(msg))
	n, err := c2.Read(buf)
	if err != nil {
		t.Fatalf("Read from pipe failed: %v", err)
	}
	if string(buf[:n]) != msg {
		t.Errorf("Expected %s, got %s", msg, string(buf[:n]))
	}
	<-done
}
