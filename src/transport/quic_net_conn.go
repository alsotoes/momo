package transport

import (
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
)

// QUICNetConn wraps a quic.Stream and quic.Connection to implement net.Conn
type QUICNetConn struct {
	*quic.Stream
	conn *quic.Conn
}

// NewQUICNetConn adapts a QUIC stream as a net.Conn so the S3 gateway and
// momo protocol layers can treat QUIC like a plain TCP connection.
func NewQUICNetConn(stream *quic.Stream, conn *quic.Conn) net.Conn {
	return &QUICNetConn{
		Stream: stream,
		conn:   conn,
	}
}

// LocalAddr returns the local address of the underlying QUIC connection.
func (c *QUICNetConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address of the underlying QUIC connection.
func (c *QUICNetConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadline for the connection.
func (c *QUICNetConn) SetDeadline(t time.Time) error {
	return c.Stream.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future reads.
func (c *QUICNetConn) SetReadDeadline(t time.Time) error {
	return c.Stream.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future writes.
func (c *QUICNetConn) SetWriteDeadline(t time.Time) error {
	return c.Stream.SetWriteDeadline(t)
}

// Close closes the QUIC stream and the underlying connection.
func (c *QUICNetConn) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in QUICNetConn.Close: %v: %w", r, syscall.EIO)
		}
	}()
	err = c.Stream.Close()
	c.conn.CloseWithError(0, "")
	return err
}
