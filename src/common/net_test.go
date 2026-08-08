package common

import (
	"errors"
	"io"
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
	deadlineSet      bool
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

func (m *mockConn) SetDeadline(t time.Time) error {
	m.deadlineSet = true
	return nil
}

func (m *mockConn) Close() error {
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

func TestIdleTimeoutConn_ReadPreservesEOF(t *testing.T) {
	defer goleak.VerifyNone(t)

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "io.EOF", err: io.EOF},
		{name: "io.ErrUnexpectedEOF", err: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mc := &mockConn{readError: tc.err}
			idleConn := NewIdleTimeoutConn(mc, 30*time.Second)

			_, err := idleConn.Read([]byte("test"))
			if err != tc.err {
				t.Fatalf("Expected err to be identity-equal to %v (direct == check), got %v", tc.err, err)
			}
		})
	}
}

func TestIdleTimeoutConn_SlowProgressiveReadSurvives(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Regression for #614: a slow-but-progressing connection must not be cut
	// because the deadline amortization was call-count based (1 byte/sec over
	// 64 bytes = 64s > timeout). With a time-based refresh window the deadline
	// tracks real data progress, so a drip-fed stream lives on.

	client, server := net.Pipe()
	idleConn := NewIdleTimeoutConn(client, 50*time.Millisecond)

	const totalBytes = 50
	const dripInterval = 4 * time.Millisecond

	go func() {
		defer server.Close()
		buf := make([]byte, 1)
		for range totalBytes {
			if _, err := server.Write(buf); err != nil {
				return
			}
			time.Sleep(dripInterval)
		}
	}()

	defer func() {
		idleConn.Close()
		client.Close()
	}()

	// Total window: 50 * 4ms = 200ms. With the old call-count logic the first
	// (and only until the 64th) deadline refresh would have expired the 50ms
	// timeout ~13 bytes in; time-based refresh must survive the whole stream.
	readBuf := make([]byte, 1)
	wallClock := time.After(2 * time.Second)
	for bytesRead := 0; bytesRead < totalBytes; {
		select {
		case <-wallClock:
			t.Fatalf("test timed out after %d bytes", bytesRead)
		default:
		}
		if _, err := idleConn.Read(readBuf); err != nil {
			if isTimeout(err) {
				t.Fatalf("slow progressive read hit deadline at byte %d: %v", bytesRead, err)
			}
			return
		}
		bytesRead++
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

func TestIdleTimeoutConn_AbsoluteDeadlineConcurrentAccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	mc := &mockConn{}
	idleConn := NewIdleTimeoutConn(mc, 30*time.Second)

	var wg sync.WaitGroup
	wg.Add(2)

	stop := make(chan struct{})

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				idleConn.SetAbsoluteDeadline(time.Now().Add(time.Hour))
			}
		}
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 64)
		for {
			select {
			case <-stop:
				return
			default:
				idleConn.Read(buf)
				idleConn.Write(buf)
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
