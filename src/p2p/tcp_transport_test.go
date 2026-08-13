package p2p

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTCPTransport_ListenDial(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	if err := tr.Listen(addr); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	if tr.Addr() == "" {
		t.Error("Addr() should return non-empty after Listen")
	}

	peer, err := tr.Dial(2, addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	if peer.ID != 2 {
		t.Errorf("expected peer ID 2, got %d", peer.ID)
	}

	if tr.Peers().Count() < 1 {
		t.Error("expected at least 1 peer after dial")
	}
}

func TestTCPTransport_SendReceive(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()

	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	if err := tr1.Listen(addr1); err != nil {
		t.Fatalf("tr1 Listen failed: %v", err)
	}
	if err := tr2.Listen(addr2); err != nil {
		t.Fatalf("tr2 Listen failed: %v", err)
	}

	if _, err := tr1.Dial(2, addr2); err != nil {
		t.Fatalf("tr1 Dial failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("ping"),
	}

	if err := tr1.Send(2, rpc); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case received := <-tr2.Consume():
		if received.From != 1 {
			t.Errorf("expected From=1, got %d", received.From)
		}
		if received.Type != MsgHeartbeat {
			t.Errorf("expected Type=MsgHeartbeat, got %d", received.Type)
		}
		if string(received.Payload) != "ping" {
			t.Errorf("expected payload 'ping', got %q", received.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RPC")
	}
}

func TestTCPTransport_Broadcast(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	tr3 := NewTCPTransport(TCPTransportConfig{LocalID: 3})
	defer tr1.Close()
	defer tr2.Close()
	defer tr3.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()

	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	ln3, _ := net.Listen("tcp", "127.0.0.1:0")
	addr3 := ln3.Addr().String()
	ln3.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)
	tr3.Listen(addr3)

	tr1.Dial(2, addr2)
	tr1.Dial(3, addr3)

	time.Sleep(100 * time.Millisecond)

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("broadcast"),
	}

	sent := tr1.Broadcast(rpc)
	if sent < 2 {
		t.Errorf("expected at least 2 sends, got %d", sent)
	}
}

func TestTCPTransport_Connect(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln.Addr().String()
	ln.Close()

	if err := tr2.Listen(addr2); err != nil {
		t.Fatalf("tr2 Listen failed: %v", err)
	}

	peer := NewPeer(2, addr2)
	if peer.Conn() != nil {
		t.Fatal("expected new peer to have no connection")
	}
	tr1.Peers().Add(peer)

	if err := tr1.Connect(peer); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if peer.Conn() == nil {
		t.Fatal("expected peer to have a connection after Connect")
	}

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("connected"),
	}
	if err := tr1.Send(2, rpc); err != nil {
		t.Fatalf("Send after Connect failed: %v", err)
	}

	select {
	case received := <-tr2.Consume():
		if received.From != 1 {
			t.Errorf("expected From=1, got %d", received.From)
		}
		if string(received.Payload) != "connected" {
			t.Errorf("expected payload 'connected', got %q", received.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for RPC after Connect")
	}
}

func TestTCPTransport_ConnectFailsOnUnreachable(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr1.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	unreachable := ln.Addr().String()
	ln.Close()

	peer := NewPeer(9, unreachable)
	tr1.Peers().Add(peer)
	if err := tr1.Connect(peer); err == nil {
		t.Fatal("expected Connect to fail for unreachable address")
	}
	if peer.Conn() != nil {
		t.Error("expected peer to remain unconnected after failed Connect")
	}
}

func generateTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "momo-p2p-test"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	pool := x509.NewCertPool()
	parsedCert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	pool.AddCert(parsedCert)

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
}

func TestTCPTransport_TLS(t *testing.T) {
	tlsConfig := generateTestTLSConfig(t)

	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1, TLSConfig: tlsConfig})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2, TLSConfig: tlsConfig})
	defer tr1.Close()
	defer tr2.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln.Addr().String()
	ln.Close()

	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	if err := tr1.Listen(addr1); err != nil {
		t.Fatalf("tr1 Listen failed: %v", err)
	}
	if err := tr2.Listen(addr2); err != nil {
		t.Fatalf("tr2 Listen failed: %v", err)
	}

	if _, err := tr1.Dial(2, addr2); err != nil {
		t.Fatalf("tr1 TLS Dial failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	rpc := &RPC{
		From:    1,
		Type:    MsgHeartbeat,
		Payload: []byte("tls-ping"),
	}

	if err := tr1.Send(2, rpc); err != nil {
		t.Fatalf("TLS Send failed: %v", err)
	}

	select {
	case received := <-tr2.Consume():
		if string(received.Payload) != "tls-ping" {
			t.Errorf("expected payload 'tls-ping', got %q", received.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for TLS RPC")
	}
}

func TestTCPTransport_DialAfterClose(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	if err := tr.Listen(addr); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, err := tr.Dial(2, addr)
	if err == nil {
		t.Fatal("expected error when Dialing after Close")
	}
}

func TestTCPTransport_ConcurrentDialClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})

		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := ln.Addr().String()
		ln.Close()

		if err := tr.Listen(addr); err != nil {
			t.Fatalf("Listen failed: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			tr.Dial(2, addr)
		}()

		go func() {
			defer wg.Done()
			tr.Close()
		}()

		wg.Wait()
	}
}

func TestTCPTransport_AuthFunc(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{
		LocalID:  1,
		AuthFunc: func(id int32) bool { return id == 2 },
	})
	defer tr.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	if err := tr.Listen(addr); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	if _, err := tr.Dial(2, addr); err != nil {
		t.Fatalf("Dial for authorized peer 2 failed: %v", err)
	}
}

// TestTCPTransport_PeerDisconnectCleanup verifies that when a connection ends,
// the closed net.Conn is removed from the transport's tracked connection set
// and detached from the peer, preventing a memory leak and writes to stale
// closed connections (issue #631).
func TestTCPTransport_PeerDisconnectCleanup(t *testing.T) {
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr2.Close()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	if err := tr2.Listen(addr); err != nil {
		t.Fatalf("tr2 Listen failed: %v", err)
	}

	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr1.Close()

	peer, err := tr1.Dial(2, addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	if peer.Conn() == nil {
		t.Fatal("expected live connection after dial")
	}

	// Give the read loop time to register the conn.
	deadline := time.Now().Add(2 * time.Second)
	for {
		tr1.mu.Lock()
		n := len(tr1.conns)
		tr1.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected a tracked connection after dial")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Force disconnect by closing the connection.
	peer.Conn().Close()

	// After the read loop exits, the conn must be removed and detached.
	deadline = time.Now().Add(2 * time.Second)
	for {
		tr1.mu.Lock()
		n := len(tr1.conns)
		tr1.mu.Unlock()
		if n == 0 && peer.Conn() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("conn not cleaned up after disconnect: tracked=%d, peerConn=nil?%v", n, peer.Conn() == nil)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
