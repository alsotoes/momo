package common

import (
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// QUICNetConn wraps a quic.Stream and quic.Connection to implement net.Conn
type QUICNetConn struct {
	*quic.Stream
	conn *quic.Conn
}

func NewQUICNetConn(stream *quic.Stream, conn *quic.Conn) net.Conn {
	return &QUICNetConn{
		Stream: stream,
		conn:   conn,
	}
}

func (c *QUICNetConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *QUICNetConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *QUICNetConn) SetDeadline(t time.Time) error {
	return c.Stream.SetDeadline(t)
}

func (c *QUICNetConn) SetReadDeadline(t time.Time) error {
	return c.Stream.SetReadDeadline(t)
}

func (c *QUICNetConn) SetWriteDeadline(t time.Time) error {
	return c.Stream.SetWriteDeadline(t)
}
