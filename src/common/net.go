package common

import (
	"errors"
	"net"
	"time"
)

// IdleTimeoutConn wraps a net.Conn to provide a rolling idle timeout.
// Every successful Read or Write resets the deadline, preventing slowloris
// attacks without interrupting large, active file transfers.
type IdleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

// NewIdleTimeoutConn creates a new IdleTimeoutConn.
func NewIdleTimeoutConn(conn net.Conn, timeout time.Duration) *IdleTimeoutConn {
	return &IdleTimeoutConn{Conn: conn, timeout: timeout}
}

// Read reads data from the connection and resets the read deadline.
func (c *IdleTimeoutConn) Read(b []byte) (n int, err error) {
	c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
	n, err = c.Conn.Read(b)
	return n, err
}

// Write writes data to the connection and resets the write deadline.
func (c *IdleTimeoutConn) Write(b []byte) (n int, err error) {
	c.Conn.SetWriteDeadline(time.Now().Add(c.timeout))
	n, err = c.Conn.Write(b)
	return n, err
}

// DialSocket connects to the given address.
// It returns a net.Conn or an error.
func DialSocket(servAddr string) (net.Conn, error) {
	// 🛡️ Sentinel: Enforce a timeout to prevent DoS via hanging outbound connections
	connection, err := net.DialTimeout("tcp", servAddr, 10*time.Second)
	if err != nil {
		return nil, errors.New("Dial failed: " + err.Error())
	}

	return connection, nil
}
