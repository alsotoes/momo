package common

import (
	"context"
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

// Communicator defines a transport-agnostic interface for Momo protocol operations.
// It encapsulates the handshake, metadata exchange, and file transfer logic.
type Communicator interface {
	io.ReadWriteCloser
	
	// SetAbsoluteDeadline sets a hard deadline for all subsequent operations.
	SetAbsoluteDeadline(t interface{}) error

	// HandshakeClient performs the client-side handshake: sends AuthToken + Timestamp,
	// and receives the negotiated replication mode.
	HandshakeClient(authToken string, timestamp int64) (int, error)

	// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp,
	// validates the token, and returns the timestamp.
	HandshakeServer(expectedAuthToken []byte) (replicationMode int, timestamp int64, err error)

	// SendReplicationMode sends the chosen replication mode back to the client.
	SendReplicationMode(mode int) error

	// SendMetadata sends file metadata (Hash, Name, Size) to the peer.

	SendMetadata(meta *FileMetadata) error

	// ReceiveMetadata receives file metadata from the peer.
	ReceiveMetadata() (FileMetadata, error)

	// SendACK sends a server acknowledgment to the client.
	SendACK(serverId int) error

	// ReceiveACK waits for a server acknowledgment.
	ReceiveACK() error

	// RemoteAddr returns the address of the remote peer.
	RemoteAddr() net.Addr
}

// MomoListener defines a transport-agnostic interface for accepting new Momo connections.
type MomoListener interface {
	Accept() (Communicator, error)
	Close() error
	Addr() net.Addr
}

// TCPListener wraps a standard net.Listener to implement the MomoListener interface.
type TCPListener struct {
	net.Listener
	factory *ProtocolFactory
}

func (l *TCPListener) Accept() (Communicator, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.factory.NewCommunicator(conn)
}

// QUICListener wraps a quic.Listener to implement the MomoListener interface.
type QUICListener struct {
	*quic.Listener
	factory *ProtocolFactory
}

func (l *QUICListener) Accept() (Communicator, error) {
	conn, err := l.Listener.Accept(context.Background())
	if err != nil {
		return nil, err
	}
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		return nil, err
	}
	if l.factory.cfg.Global.Protocol == "s3-quic" {
		return NewS3Communicator(NewQUICNetConn(stream, conn)), nil
	}
	return NewMomoQUICCommunicator(stream, conn), nil
}
