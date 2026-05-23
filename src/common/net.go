package common

import (
	"errors"
	"net"
	"sync/atomic"
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
	lastReadUpdate   atomic.Int64
	lastWriteUpdate  atomic.Int64
}

// NewIdleTimeoutConn creates a new IdleTimeoutConn.
func NewIdleTimeoutConn(conn net.Conn, timeout time.Duration) *IdleTimeoutConn {
	return &IdleTimeoutConn{Conn: conn, timeout: timeout}
}

// SetAbsoluteDeadline sets an absolute hard deadline for the connection.
// If the absolute deadline is reached, reads and writes will fail regardless of idle activity.
func (c *IdleTimeoutConn) SetAbsoluteDeadline(t time.Time) {
	c.absoluteDeadline = t
}

func (c *IdleTimeoutConn) applyDeadlines(isRead bool) {
	var lastUpdate *atomic.Int64
	if isRead {
		lastUpdate = &c.lastReadUpdate
	} else {
		lastUpdate = &c.lastWriteUpdate
	}

	now := time.Now()
	nowNano := now.UnixNano()

	// ⚡ Bolt: Only update the deadline if at least 1/4 of the timeout has elapsed.
	// This drastically reduces the number of SetDeadline system calls during high-frequency I/O.
	last := lastUpdate.Load()
	if last != 0 && nowNano-last < int64(c.timeout/4) {
		return
	}

	deadline := now.Add(c.timeout)
	if !c.absoluteDeadline.IsZero() && c.absoluteDeadline.Before(deadline) {
		deadline = c.absoluteDeadline
	}

	if isRead {
		c.Conn.SetReadDeadline(deadline)
	} else {
		c.Conn.SetWriteDeadline(deadline)
	}
	lastUpdate.Store(nowNano)
}

// Read reads data from the connection and resets the read deadline.
func (c *IdleTimeoutConn) Read(b []byte) (n int, err error) {
	c.applyDeadlines(true)
	n, err = c.Conn.Read(b)
	return n, err
}

// Write writes data to the connection and resets the write deadline.
func (c *IdleTimeoutConn) Write(b []byte) (n int, err error) {
	c.applyDeadlines(false)
	n, err = c.Conn.Write(b)
	return n, err
}

// DialSocket connects to the given address.
// It returns a net.Conn or an error.
func DialSocket(servAddr string) (net.Conn, error) {
	connection, err := net.DialTimeout("tcp", servAddr, 10*time.Second)
	if err != nil {
		return nil, errors.New("Dial failed: " + err.Error())
	}

	// 🛡️ Sentinel: Wrap outbound connections with an idle timeout to prevent goroutine leaks
	// and Denial of Service (DoS) from malicious or unresponsive peers.
	return NewIdleTimeoutConn(connection, 30*time.Second), nil
}
