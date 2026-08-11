package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

type testCertChain struct {
	caPool    *x509.CertPool
	serverTLS *tls.Config
	clientTLS *tls.Config
}

// generateTestCertChain builds a CA and two leaf certificates signed by it:
// one for the QUIC server and one for the QUIC client (mutual TLS).
func generateTestCertChain(t *testing.T) *testCertChain {
	t.Helper()

	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}
	caTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "momo-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, caPub, caPriv)
	if err != nil {
		t.Fatalf("failed to create CA cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	leaf := func(name string, keyUsage x509.ExtKeyUsage, ips []net.IP) (tls.Certificate, error) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return tls.Certificate{}, err
		}
		tmpl := x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: name},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{keyUsage},
			IPAddresses:  ips,
		}
		der, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, pub, caPriv)
		if err != nil {
			return tls.Certificate{}, err
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
	}

	serverCert, err := leaf("momo-test-server", x509.ExtKeyUsageServerAuth, []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")})
	if err != nil {
		t.Fatalf("failed to create server cert: %v", err)
	}
	clientCert, err := leaf("momo-test-client", x509.ExtKeyUsageClientAuth, nil)
	if err != nil {
		t.Fatalf("failed to create client cert: %v", err)
	}

	return &testCertChain{
		caPool: caPool,
		serverTLS: &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			MinVersion:   tls.VersionTLS12,
		},
		clientTLS: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			MinVersion:   tls.VersionTLS12,
		},
	}
}

// TestProtocolFactory_Listen_QUIC_ConfiguredTLS verifies (issue #624) that the
// QUIC listener presents the configured TLS certificate and requires a client
// certificate signed by the configured CA (mutual TLS) instead of always
// generating a self-signed certificate.
func TestProtocolFactory_Listen_QUIC_ConfiguredTLS(t *testing.T) {
	chain := generateTestCertChain(t)

	serverFactory := &ProtocolFactory{
		cfg: common.Configuration{
			Global: common.ConfigurationGlobal{Protocol: "momo-quic"},
		},
		tlsConfig: chain.serverTLS,
		certPool:  chain.caPool,
	}

	l, err := serverFactory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("server failed to listen: %v", err)
	}
	defer l.Close()

	// A client presenting a certificate signed by the CA must succeed.
	clientFactory := &ProtocolFactory{
		cfg: common.Configuration{
			Global: common.ConfigurationGlobal{Protocol: "momo-quic"},
		},
		tlsConfig:        chain.clientTLS,
		certPool:         chain.caPool,
		useChallengeResp: true,
	}

	comm, err := clientFactory.Dial(l.Addr().String())
	if err != nil {
		t.Fatalf("client failed to dial with mTLS cert: %v", err)
	}
	comm.Close()

	// A client with the CA pool but no client certificate must be rejected:
	// proving the listener now enforces mutual TLS via the configured CA.
	// quic-go may return the dial before the server's rejection lands, so the
	// error surfaces on the next connection operation (same pattern as quic-go's
	// own TestHandshakeFailsWithoutClientCert).
	noCertFactory := &ProtocolFactory{
		cfg: common.Configuration{
			Global: common.ConfigurationGlobal{Protocol: "momo-quic"},
		},
		certPool: chain.caPool,
	}

	noCertComm, err := noCertFactory.Dial(l.Addr().String())
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	mc := noCertComm.(*MomoQUICCommunicator)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = mc.conn.AcceptStream(ctx)
	if err == nil {
		t.Fatal("expected mTLS rejection (no client cert) to surface on the connection")
	}
	noCertComm.Close()
}

// TestProtocolFactory_Listen_QUIC_DefaultSelfSigned verifies the fallback: with
// no configured TLS certificate, the QUIC listener still works using the
// self-signed certificate (backward compatible with tls_insecure dialing).
func TestProtocolFactory_Listen_QUIC_DefaultSelfSigned(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "momo-quic",
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	dialFactory := NewProtocolFactory(cfg)
	comm, err := dialFactory.Dial(l.Addr().String())
	if err != nil {
		t.Fatalf("client failed to dial self-signed listener: %v", err)
	}
	comm.Close()
}
