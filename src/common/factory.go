package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/quic-go/quic-go"
)

// ProtocolFactory is responsible for creating Communicator instances based on configuration.
type ProtocolFactory struct {
	cfg Configuration
}

// NewProtocolFactory creates a new ProtocolFactory.
func NewProtocolFactory(cfg Configuration) *ProtocolFactory {
	return &ProtocolFactory{cfg: cfg}
}

// NewCommunicator creates a new Communicator for the given connection based on the global protocol setting.
func (f *ProtocolFactory) NewCommunicator(conn net.Conn) (Communicator, error) {
	switch f.cfg.Global.Protocol {
	case "momo-tcp", "s3-tcp":
		return NewMomoTCPCommunicator(conn), nil
	default:
		return nil, fmt.Errorf("unsupported protocol for raw connection: %q", f.cfg.Global.Protocol)
	}
}

// Dial connects to a peer using the configured protocol and returns a Communicator.
func (f *ProtocolFactory) Dial(address string) (Communicator, error) {
	switch f.cfg.Global.Protocol {
	case "momo-tcp", "s3-tcp":
		conn, err := DialSocket(address)
		if err != nil {
			return nil, err
		}
		return f.NewCommunicator(conn)
	case "momo-quic", "s3-quic":
		conn, stream, err := DialQUIC(context.Background(), address)
		if err != nil {
			return nil, err
		}
		return NewMomoQUICCommunicator(stream, conn), nil
	default:
		return nil, fmt.Errorf("unsupported protocol for dialing: %q", f.cfg.Global.Protocol)
	}
}

// Listen starts a listener for the configured protocol.
func (f *ProtocolFactory) Listen(address string) (MomoListener, error) {
	switch f.cfg.Global.Protocol {
	case "momo-tcp", "s3-tcp":
		l, err := net.Listen("tcp", address)
		if err != nil {
			return nil, err
		}
		return &TCPListener{Listener: l, factory: f}, nil
	case "momo-quic", "s3-quic":
		cert, err := GenerateSelfSignedCert()
		if err != nil {
			return nil, fmt.Errorf("failed to generate cert: %w", err)
		}
		tlsConf := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"momo-quic"},
		}
		l, err := quic.ListenAddr(address, tlsConf, nil)
		if err != nil {
			return nil, err
		}
		return &QUICListener{Listener: l, factory: f}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol for listening: %q", f.cfg.Global.Protocol)
	}
}

// GetDaemons returns the list of daemons from the configuration.
func (f *ProtocolFactory) GetDaemons() []*Daemon {
	return f.cfg.Daemons
}

// GetAuthToken returns the global AuthToken.
func (f *ProtocolFactory) GetAuthToken() string {
	return f.cfg.Global.AuthToken
}
