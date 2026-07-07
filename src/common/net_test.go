package common

import (
	"net"
	"testing"
	"time"
)

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

func TestIdleTimeoutConn_WriteTimeoutEdgeCase(t *testing.T) {
	// The only way to trigger a timeout on Write is if the underlying
	// connection blocks because the reading end is not consuming data,
	// causing the write to exceed the deadline set by IdleTimeoutConn.

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Set a very short timeout
	idleConn := NewIdleTimeoutConn(client, 20*time.Millisecond)

	errCh := make(chan error, 1)
	go func() {
		// Because no one is reading from `server`, this write will block.
		// `IdleTimeoutConn.Write` will extend the deadline by 20ms right before writing,
		// but since it blocks, it will time out after 20ms.
		_, err := idleConn.Write([]byte("this_will_block"))
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Expected timeout error on blocking write, got nil")
		} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			t.Fatalf("Expected timeout error, got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Test timed out waiting for Write to fail with a deadline timeout")
	}
}

func TestDialSocket(t *testing.T) {
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
