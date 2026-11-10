package transport

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// maxLimitedReaderExhaustions bounds how many times the racing reader may see
// an exhausted limit before the test declares the setter stuck.
const maxLimitedReaderExhaustions = 10

// TestLimitedConnReaderConcurrent races SetLimit/ClearLimit against Read from
// separate goroutines. It must complete without deadlock and pass under
// -race (issue #652).
func TestLimitedConnReaderConcurrent(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	lr := &LimitedConnReader{r: server}
	const total = 200_000
	go func() {
		buf := make([]byte, 4096)
		remain := total
		for remain > 0 {
			w := len(buf)
			if w > remain {
				w = remain
			}
			if _, err := client.Write(buf[:w]); err != nil {
				return
			}
			remain -= w
		}
	}()

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for i := 0; i < 300; i++ {
			lr.SetLimit(65536)
			lr.ClearLimit()
		}
	}()

	buf := make([]byte, 4096)
	got := 0
	exhaustions := 0
	for got < total {
		n, err := lr.Read(buf)
		got += n
		if err == nil {
			continue
		}
		if !errors.Is(err, syscall.ENOBUFS) {
			t.Fatalf("read failed after %d/%d bytes: %v", got, total, err)
		}
		// SetLimit installs a real 65536-byte budget, so ENOBUFS while the
		// setter is mid-cycle is the correct answer. Synchronise on the setter
		// finishing (it ends with ClearLimit) instead of polling, and bound the
		// retries so a regression that never clears the limit fails loudly
		// rather than hanging.
		exhaustions++
		if exhaustions > maxLimitedReaderExhaustions {
			t.Fatalf("read limit never cleared after %d exhaustions (%d/%d bytes)", exhaustions, got, total)
		}
		<-stop
	}
}

// TestLimitedConnReaderEnforcesLimit verifies the read window is bounded and
// that an exhausted limit surfaces as ENOBUFS.
func TestLimitedConnReaderEnforcesLimit(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	lr := &LimitedConnReader{r: server}
	lr.SetLimit(32)
	go func() {
		_, _ = client.Write(make([]byte, 128))
	}()

	buf := make([]byte, 128)
	n, err := lr.Read(buf)
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if n != 32 {
		t.Fatalf("expected read limited to 32 bytes, got %d", n)
	}

	if _, err = lr.Read(buf); !errors.Is(err, syscall.ENOBUFS) {
		t.Fatalf("expected syscall.ENOBUFS after exhausting limit, got %v", err)
	}

	lr.ClearLimit()
	if n, err = lr.Read(buf); err != nil || n == 0 {
		t.Fatalf("expected reads to resume after ClearLimit, got n=%d err=%v", n, err)
	}
}
