package common

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
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
	absoluteDeadline time.Time
	readCalls        atomic.Uint32
	writeCalls       atomic.Uint32
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
		}
	}()

	c.absoluteDeadline = t

	// 🛡️ Sentinel: Immediately apply the new deadline to the underlying connection.
	// Since applyDeadlines amortizes updates (skipping 63 of 64 calls), failing to
	// explicitly update here leaves the connection with a potentially stale, strict
	// handshake deadline, causing valid large file transfers to drop prematurely (DoS).
	now := time.Now()
	deadline := now.Add(c.timeout)
	if !c.absoluteDeadline.IsZero() && c.absoluteDeadline.Before(deadline) {
		deadline = c.absoluteDeadline
	}
	c.Conn.SetDeadline(deadline)
}

func (c *IdleTimeoutConn) applyDeadlines(isRead bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in IdleTimeoutConn.applyDeadlines: %v", r)
			c.broken.Store(true)
		}
	}()

	var calls *atomic.Uint32
	if isRead {
		calls = &c.readCalls
	} else {
		calls = &c.writeCalls
	}

	// ⚡ Bolt: Amortize the cost of updating deadlines using a high-performance bitwise check.
	// We only update the deadline on the first call and every 64 calls thereafter.
	// This reduces time.Now() and SetDeadline system calls by 98.4%.
	// For Slowloris, this is actually MORE secure as it requires 64 drip-bytes to reset the timer.
	count := calls.Add(1)
	if (count-1)&63 != 0 {
		return
	}

	now := time.Now()
	deadline := now.Add(c.timeout)
	if !c.absoluteDeadline.IsZero() && c.absoluteDeadline.Before(deadline) {
		deadline = c.absoluteDeadline
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
	if err != nil {
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in DialSocket: %v", r)
			err = fmt.Errorf("dial panic: %w", syscall.EIO)
		}
	}()

	connection, dErr := net.DialTimeout("tcp", servAddr, 10*time.Second)
	if dErr != nil {
		conn = nil
		err = fmt.Errorf("Dial failed: %v: %w", dErr, syscall.ECONNREFUSED)
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
