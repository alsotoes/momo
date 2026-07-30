package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/quic-go/quic-go"
)

const quicDialTimeout = 10 * time.Second

// ProtocolFactory is responsible for creating Communicator instances based on configuration.
type ProtocolFactory struct {
	cfg      common.Configuration
	certPool *x509.CertPool
}

// NewProtocolFactory creates a new ProtocolFactory.
func NewProtocolFactory(cfg common.Configuration) *ProtocolFactory {
	f := &ProtocolFactory{cfg: cfg}
	if cfg.Global.CACertPath != "" {
		pemData, err := os.ReadFile(cfg.Global.CACertPath)
		if err != nil {
			log.Printf("WARNING: failed to read CA cert file %s: %v — falling back to InsecureSkipVerify", cfg.Global.CACertPath, err)
			return f
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			log.Printf("WARNING: failed to parse CA cert from %s — falling back to InsecureSkipVerify", cfg.Global.CACertPath)
			return f
		}
		f.certPool = pool
	}
	return f
}

// NewCommunicator creates a new Communicator for the given connection based on the global protocol setting.
func (f *ProtocolFactory) NewCommunicator(conn net.Conn) (Communicator, error) {
	switch f.cfg.Global.Protocol {
	case "momo-tcp":
		return NewMomoTCPCommunicator(conn), nil
	case "s3-tcp":
		return NewS3Communicator(conn), nil
	default:
		return nil, fmt.Errorf("unsupported protocol for raw connection: %q", f.cfg.Global.Protocol)
	}
}

// Dial connects to a peer using the configured protocol and returns a Communicator.
func (f *ProtocolFactory) Dial(address string) (Communicator, error) {
	switch f.cfg.Global.Protocol {
	case "momo-tcp", "s3-tcp":
		conn, err := common.DialSocket(address)
		if err != nil {
			return nil, err
		}
		return f.NewCommunicator(conn)
	case "momo-quic", "s3-quic":
		ctx, cancel := context.WithTimeout(context.Background(), quicDialTimeout)
		defer cancel()
		conn, stream, err := DialQUIC(ctx, address, f.certPool)
		if err != nil {
			return nil, err
		}
		if f.cfg.Global.Protocol == "s3-quic" {
			return NewS3Communicator(NewQUICNetConn(stream, conn)), nil
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
func (f *ProtocolFactory) GetDaemons() []*common.Daemon {
	return f.cfg.Daemons
}

// GetAuthToken returns the global AuthToken.
func (f *ProtocolFactory) GetAuthToken() string {
	return f.cfg.Global.AuthToken
}
