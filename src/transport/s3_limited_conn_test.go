package transport

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

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
	for got < total {
		n, err := lr.Read(buf)
		got += n
		if err != nil {
			t.Fatalf("read failed after %d/%d bytes: %v", got, total, err)
		}
	}
	<-stop
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
