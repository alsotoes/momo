package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// IdleTimeoutConn wraps a net.Conn to provide a rolling idle timeout.
// Every successful Read or Write resets the deadline, preventing slowloris
// attacks without interrupting large, active file transfers.
// An optional absolute deadline can be set to enforce a maximum connection duration.
type IdleTimeoutConn struct {
	net.Conn
	timeout          time.Duration
	absoluteDeadline atomic.Pointer[time.Time]
	lastReadRefresh  atomic.Int64
	lastWriteRefresh atomic.Int64
	broken           atomic.Bool
}

// NewIdleTimeoutConn creates a new IdleTimeoutConn.
func NewIdleTimeoutConn(conn net.Conn, timeout time.Duration) *IdleTimeoutConn {
	return &IdleTimeoutConn{Conn: conn, timeout: timeout}
}

// SetAbsoluteDeadline sets an absolute hard deadline for the connection.
// If the absolute deadline is reached, reads and writes will fail regardless of idle activity.
func (c *IdleTimeoutConn) SetAbsoluteDeadline(t time.Time) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in IdleTimeoutConn.SetAbsoluteDeadline: %v", r)
			c.broken.Store(true)
			if c.Conn != nil {
				c.Conn.Close()
			}
		}
	}()

	c.absoluteDeadline.Store(&t)

	// 🛡️ Sentinel: Immediately apply the new deadline to the underlying connection.
	// Since applyDeadlines amortizes updates (skipping 63 of 64 calls), failing to
	// explicitly update here leaves the connection with a potentially stale, strict
	// handshake deadline, causing valid large file transfers to drop prematurely (DoS).
	now := time.Now()
	deadline := now.Add(c.timeout)
	if !t.IsZero() && t.Before(deadline) {
		deadline = t
	}
	c.Conn.SetDeadline(deadline)
}

func (c *IdleTimeoutConn) applyDeadlines(isRead bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in IdleTimeoutConn.applyDeadlines: %v", r)
			c.broken.Store(true)
			if c.Conn != nil {
				c.Conn.Close()
			}
		}
	}()

	var lastRefresh *atomic.Int64
	if isRead {
		lastRefresh = &c.lastReadRefresh
	} else {
		lastRefresh = &c.lastWriteRefresh
	}

	now := time.Now()
	// ⚡ Bolt: Amortize the cost of updating deadlines by refreshing, at most,
	// once per amortization window based on elapsed time rather than call count.
	// Counting calls (e.g. every 64th call) breaks slow-but-progressing
	// connections: a link delivering 1 byte/sec only advances the deadline every
	// 64 seconds, exceeding a 30s timeout and killing a live transfer. A time
	// window keeps the deadline within `refresh` of real data progress, so
	// healthy slow links survive while idle connections still time out. This
	// still avoids time.Now()/SetDeadline syscalls on high-throughput paths.
	refresh := c.timeout / 4
	if prev := lastRefresh.Load(); prev != 0 && now.Sub(time.Unix(0, prev)) < refresh {
		return
	}
	lastRefresh.Store(now.UnixNano())

	deadline := now.Add(c.timeout)
	if dp := c.absoluteDeadline.Load(); dp != nil && !dp.IsZero() && dp.Before(deadline) {
		deadline = *dp
	}

	if isRead {
		c.Conn.SetReadDeadline(deadline)
	} else {
		c.Conn.SetWriteDeadline(deadline)
	}
}

// Read reads data from the connection and resets the read deadline.
func (c *IdleTimeoutConn) Read(b []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in IdleTimeoutConn.Read: %v", r)
			err = fmt.Errorf("read panic: %w", syscall.EIO)
		}
	}()

	if c.broken.Load() {
		return 0, fmt.Errorf("connection broken: %w", syscall.EIO)
	}

	c.applyDeadlines(true)
	n, err = c.Conn.Read(b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		if isTimeout(err) {
			err = fmt.Errorf("%w: %w", err, syscall.ETIMEDOUT)
		} else {
			err = fmt.Errorf("%w: %w", err, syscall.ECONNABORTED)
		}
	}
	return n, err
}

// Write writes data to the connection and resets the write deadline.
func (c *IdleTimeoutConn) Write(b []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in IdleTimeoutConn.Write: %v", r)
			err = fmt.Errorf("write panic: %w", syscall.EIO)
		}
	}()

	if c.broken.Load() {
		return 0, fmt.Errorf("connection broken: %w", syscall.EIO)
	}

	c.applyDeadlines(false)
	n, err = c.Conn.Write(b)
	if err != nil {
		if isTimeout(err) {
			err = fmt.Errorf("%w: %w", err, syscall.ETIMEDOUT)
		} else {
			err = fmt.Errorf("%w: %w", err, syscall.EIO)
		}
	}
	return n, err
}

// DialSocket connects to the given address.
// It returns a net.Conn or an error.
func DialSocket(servAddr string) (conn net.Conn, err error) {
	return DialSocketWithContext(context.Background(), servAddr)
}

// DialSocketWithContext connects to the given address with a context-derived timeout.
// If the context has a deadline, it is used; otherwise a 10s default is applied.
func DialSocketWithContext(ctx context.Context, servAddr string) (conn net.Conn, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in DialSocketWithContext: %v", r)
			err = fmt.Errorf("dial panic: %w", syscall.EIO)
		}
	}()

	dialer := net.Dialer{Timeout: 10 * time.Second}
	connection, dErr := dialer.DialContext(ctx, "tcp", servAddr)
	if dErr != nil {
		conn = nil
		if isTimeout(dErr) {
			err = fmt.Errorf("Dial timeout: %v: %w", dErr, syscall.ETIMEDOUT)
		} else {
			errStr := dErr.Error()
			if strings.Contains(errStr, "connection refused") || errors.Is(dErr, syscall.ECONNREFUSED) {
				err = fmt.Errorf("Dial refused: %v: %w", dErr, syscall.ECONNREFUSED)
			} else if strings.Contains(errStr, "no route to host") || errors.Is(dErr, syscall.EHOSTUNREACH) {
				err = fmt.Errorf("Dial host unreachable: %v: %w", dErr, syscall.EHOSTUNREACH)
			} else {
				err = fmt.Errorf("Dial aborted: %v: %w", dErr, syscall.ECONNABORTED)
			}
		}
		return conn, err
	}

	// 🛡️ Sentinel: Wrap outbound connections with an idle timeout to prevent goroutine leaks
	// and Denial of Service (DoS) from malicious or unresponsive peers.
	conn = NewIdleTimeoutConn(connection, 30*time.Second)
	err = nil
	return conn, err
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	return false
}
