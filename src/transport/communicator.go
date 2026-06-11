package transport

import (
	"context"
	"io"
	"net"

	"github.com/alsotoes/momo/src/common"
	"github.com/quic-go/quic-go"
)

const (
	// MetadataStatusSendPayload indicates the client should proceed with file transfer.
	MetadataStatusSendPayload = 1
	// MetadataStatusSkipPayload indicates the server already has the content (deduplication).
	MetadataStatusSkipPayload = 2
)

// Communicator defines a transport-agnostic interface for Momo protocol operations.
// It encapsulates the handshake, metadata exchange, and file transfer logic.
type Communicator interface {
	io.ReadWriteCloser

	// SetAbsoluteDeadline sets a hard deadline for all subsequent operations.
	SetAbsoluteDeadline(t interface{}) error

	// HandshakeClient performs the client-side handshake: sends AuthToken + Timestamp + RequestedMode,
	// and receives the confirmed replication mode from the server.
	HandshakeClient(authToken string, timestamp int64, requestedMode int) (replicationMode int, err error)

	// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp + RequestedMode,
	// validates the token, and returns the timestamp and requested mode.
	HandshakeServer(expectedAuthToken []byte) (replicationMode int, timestamp int64, err error)


	// SendReplicationMode sends the chosen replication mode back to the client.
	SendReplicationMode(mode int) error

	// SendMetadata sends file metadata (Hash, Name, Size) to the peer
	// and returns a status code (MetadataStatusSendPayload or MetadataStatusSkipPayload).
	SendMetadata(meta *common.FileMetadata) (int, error)

	// ReceiveMetadata receives file metadata from the peer.
	ReceiveMetadata() (common.FileMetadata, error)

	// SendMetadataStatus sends the status code back to the client after receiving metadata.
	SendMetadataStatus(status int) error

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
