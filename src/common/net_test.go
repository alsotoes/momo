package common

import (
	"errors"
	"net"
	"sync"
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

func TestIdleTimeoutConn_WriteTimeoutEdgeCase(t *testing.T) {
	defer goleak.VerifyNone(t)

	// The only way to trigger a timeout on Write is if the underlying
	// connection blocks because the reading end is not consuming data,
	// causing the write to exceed the deadline set by IdleTimeoutConn.

	client, server := net.Pipe()

	// Set a very short timeout
	idleConn := NewIdleTimeoutConn(client, 20*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)

	errCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		// Because no one is reading from `server`, this write will block.
		// `IdleTimeoutConn.Write` will extend the deadline by 20ms right before writing,
		// but since it blocks, it will time out after 20ms.
		_, err := idleConn.Write([]byte("this_will_block"))
		select {
		case errCh <- err:
		default:
		}
	}()

	defer func() {
		client.Close()
		server.Close()
		wg.Wait()
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Expected timeout error on blocking write, got nil")
		}
		if !errors.Is(err, syscall.ETIMEDOUT) {
			t.Fatalf("Expected err to wrap %v, got %v", syscall.ETIMEDOUT, err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Test timed out waiting for Write to fail with a deadline timeout")
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
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("Expected err to also wrap %v, got %v", net.ErrClosed, err)
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

type panicConn struct {
	net.Conn
	closed bool
}

func (p *panicConn) SetDeadline(t time.Time) error {
	panic("simulated deadline panic")
}

func (p *panicConn) Close() error {
	p.closed = true
	return nil
}

func TestIdleTimeoutConn_BrokenFlagAfterPanic(t *testing.T) {
	defer goleak.VerifyNone(t)

	mock := &panicConn{}
	idleConn := NewIdleTimeoutConn(mock, 30*time.Second)

	// SetAbsoluteDeadline should recover from the panic, set the broken flag, and close the connection.
	idleConn.SetAbsoluteDeadline(time.Now())

	if !mock.closed {
		t.Error("Expected connection to be closed after panic recovery, but it was not")
	}

	// Read and Write should now fail immediately with syscall.EIO.
	_, err := idleConn.Read(make([]byte, 10))
	if !errors.Is(err, syscall.EIO) {
		t.Errorf("Expected err to wrap %v, got %v", syscall.EIO, err)
	}

	_, err = idleConn.Write([]byte("test"))
	if !errors.Is(err, syscall.EIO) {
		t.Errorf("Expected err to wrap %v, got %v", syscall.EIO, err)
	}
}
