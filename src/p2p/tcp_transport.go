package p2p

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
)

const (
	p2pWriteTimeout = 5 * time.Second
	p2pReadTimeout  = 30 * time.Second
)

// TCPTransport implements the Transport interface using TCP sockets.
// It maintains a pool of peer connections and a background goroutine
// that reads RPCs from all peers and delivers them via Consume().
type TCPTransport struct {
	cfg        TCPTransportConfig
	ln         net.Listener
	listenAddr string

	peerMap *PeerMap
	conns   map[net.Conn]struct{}

	rpcCh  chan RPC
	done   chan struct{}
	closed bool
	mu     sync.Mutex
	wg     sync.WaitGroup
}

// NewTCPTransport creates a new TCPTransport with the given configuration.
func NewTCPTransport(cfg TCPTransportConfig) *TCPTransport {
	return &TCPTransport{
		cfg:     cfg,
		peerMap: NewPeerMap(),
		conns:   make(map[net.Conn]struct{}),
		rpcCh:   make(chan RPC, 256),
		done:    make(chan struct{}),
	}
}

// Listen starts accepting TCP connections on the given address.
func (t *TCPTransport) Listen(addr string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("p2p listen failed: %v: %w", err, syscall.EADDRINUSE)
	}
	if t.cfg.TLSConfig != nil {
		ln = tls.NewListener(ln, t.cfg.TLSConfig)
	}
	t.ln = ln
	t.listenAddr = ln.Addr().String()

	t.wg.Add(1)
	go t.acceptLoop()

	log.Printf("P2P transport listening on %s", t.listenAddr)
	return nil
}

// acceptLoop accepts incoming connections and spawns a read goroutine for each.
func (t *TCPTransport) acceptLoop() {
	defer t.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("P2P acceptLoop panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	for {
		conn, err := t.ln.Accept()
		if err != nil {
			select {
			case <-t.done:
				return
			default:
				log.Printf("P2P accept error: %v (errno=%d)", err, syscall.EIO)
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			conn.Close()
			continue
		}
		t.conns[conn] = struct{}{}
		t.wg.Add(1)
		t.mu.Unlock()

		go t.handleConn(conn)
	}
}

// cleanupConn removes a connection from the transport's tracked connection set
// and detaches it from its peer once the read loop for that connection exits.
// This prevents closed net.Conn objects from accumulating in t.conns (memory
// leak) and stops downstream code from writing to a stale, closed connection
// (issue #631). The peer itself stays in the peer map so gossip can track its
// liveness state.
func (t *TCPTransport) cleanupConn(conn net.Conn, peerID int32) {
	t.mu.Lock()
	delete(t.conns, conn)
	t.mu.Unlock()

	if peerID < 0 {
		return
	}
	if peer := t.peerMap.Get(peerID); peer != nil && peer.Conn() == conn {
		peer.SetConn(nil)
	}
}

// handleConn reads RPCs from a single connection and delivers them to rpcCh.
// The peer ID is extracted from the first RPC received.
func (t *TCPTransport) handleConn(conn net.Conn) (err error) {
	defer t.wg.Done()
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in handleConn: %v: %w", r, syscall.EIO)
			log.Printf("CRITICAL: %v", err)
		}
	}()

	var peer *Peer
	var peerID int32 = -1

	defer func() { t.cleanupConn(conn, peerID) }()

	for {
		conn.SetReadDeadline(time.Now().Add(p2pReadTimeout))
		rpc, decErr := DecodeRPC(conn)
		if decErr != nil {
			select {
			case <-t.done:
				return
			default:
			}
			if peerID >= 0 {
				log.Printf("P2P peer %d disconnected: %v (errno=%d)", peerID, decErr, syscall.ECONNRESET)
			}
			return
		}

		if peer == nil {
			peerID = rpc.From
			if peerID < 0 {
				log.Printf("P2P rejected invalid peer ID %d from %s (errno=%d)", peerID, conn.RemoteAddr(), syscall.EBADMSG)
				return
			}
			if t.cfg.AuthFunc != nil && !t.cfg.AuthFunc(peerID) {
				log.Printf("P2P rejected unauthenticated peer %d from %s (errno=%d)", peerID, conn.RemoteAddr(), syscall.EACCES)
				return
			}
			peer = NewPeer(peerID, conn.RemoteAddr().String())
			peer.SetConn(conn)
			t.peerMap.Add(peer)
			log.Printf("P2P peer %d connected from %s", peerID, conn.RemoteAddr())
		}

		peer.Touch()

		select {
		case t.rpcCh <- *rpc:
		case <-t.done:
			return
		}
	}
}

// dialAddr establishes a raw network connection to the given address.
func (t *TCPTransport) dialAddr(addr string) (net.Conn, error) {
	if t.cfg.TLSConfig != nil {
		return tls.Dial("tcp", addr, t.cfg.TLSConfig)
	}
	return net.DialTimeout("tcp", addr, 5*time.Second)
}

// Dial connects to a peer at the given address.
// If a peer with the same ID already exists, it returns the existing peer.
func (t *TCPTransport) Dial(id int32, addr string) (*Peer, error) {
	if existing := t.peerMap.Get(id); existing != nil {
		return existing, nil
	}

	conn, err := t.dialAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("p2p dial %s failed: %v: %w", addr, err, syscall.ECONNREFUSED)
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		conn.Close()
		return nil, fmt.Errorf("transport closed: %w", syscall.ECONNREFUSED)
	}
	t.conns[conn] = struct{}{}
	t.wg.Add(1)
	t.mu.Unlock()

	peer := NewPeer(id, addr)
	peer.SetConn(conn)
	t.peerMap.Add(peer)

	go t.readLoop(id, conn)

	log.Printf("P2P dialed peer %d at %s", id, addr)
	return peer, nil
}

// Connect establishes an outbound connection to an already-registered peer.
// It is used to wire up peers discovered via gossip membership updates that
// were added to the peer map without a live connection.
func (t *TCPTransport) Connect(peer *Peer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Connect to peer %d: %v: %w", peer.ID, r, syscall.EIO)
			log.Printf("CRITICAL: %v", err)
		}
	}()

	if peer == nil {
		return fmt.Errorf("cannot connect nil peer: %w", syscall.EINVAL)
	}
	if peer.Conn() != nil {
		return nil
	}

	conn, err := t.dialAddr(peer.Addr)
	if err != nil {
		return fmt.Errorf("p2p dial %s failed: %v: %w", peer.Addr, err, syscall.ECONNREFUSED)
	}

	// Ownership of conn transfers to the readLoop once started. Until then,
	// ensure the socket is closed on any error or panic to avoid a zombie conn.
	transferred := false
	defer func() {
		if !transferred {
			conn.Close()
		}
	}()

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport closed: %w", syscall.ECONNREFUSED)
	}
	t.conns[conn] = struct{}{}
	t.wg.Add(1)
	t.mu.Unlock()

	peer.SetConn(conn)
	go t.readLoop(peer.ID, conn)
	transferred = true

	log.Printf("P2P connected to discovered peer %d at %s", peer.ID, peer.Addr)
	return nil
}

// readLoop reads RPCs from a dialed connection.
func (t *TCPTransport) readLoop(peerID int32, conn net.Conn) {
	defer t.wg.Done()
	defer conn.Close()
	defer func() { t.cleanupConn(conn, peerID) }()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("P2P readLoop panic recovered for peer %d: %v (errno=%d)", peerID, r, syscall.EIO)
		}
	}()

	for {
		conn.SetReadDeadline(time.Now().Add(p2pReadTimeout))
		rpc, err := DecodeRPC(conn)
		if err != nil {
			select {
			case <-t.done:
				return
			default:
			}
			log.Printf("P2P peer %d read error: %v (errno=%d)", peerID, err, syscall.ECONNRESET)
			return
		}

		if peer := t.peerMap.Get(peerID); peer != nil {
			peer.Touch()
		}

		select {
		case t.rpcCh <- *rpc:
		case <-t.done:
			return
		}
	}
}

// Consume returns the channel of incoming RPCs.
func (t *TCPTransport) Consume() <-chan RPC {
	return t.rpcCh
}

// Broadcast sends an RPC to all active peers. Returns the number of peers contacted.
func (t *TCPTransport) Broadcast(rpc *RPC) (result int) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic in Broadcast: %v: %w", r, syscall.EIO)
			log.Printf("CRITICAL: %v", err)
		}
	}()

	peers := t.peerMap.All()
	encoded := rpc.Encode()
	sent := 0
	for _, p := range peers {
		if p.ID == rpc.From {
			continue
		}
		conn := p.Conn()
		if conn == nil {
			continue
		}
		func() {
			p.writeMu.Lock()
			defer p.writeMu.Unlock()
			conn.SetWriteDeadline(time.Now().Add(p2pWriteTimeout))
			if _, err := conn.Write(encoded); err != nil {
				log.Printf("P2P broadcast to peer %d failed: %v (errno=%d)", p.ID, err, syscall.EPIPE)
				return
			}
			sent++
		}()
	}
	return sent
}

// Send sends an RPC to a specific peer by ID.
func (t *TCPTransport) Send(peerID int32, rpc *RPC) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Send to peer %d: %v: %w", peerID, r, syscall.EIO)
			log.Printf("CRITICAL: %v", err)
		}
	}()

	peer := t.peerMap.Get(peerID)
	if peer == nil {
		return fmt.Errorf("peer %d not found: %w", peerID, syscall.ENOENT)
	}
	conn := peer.Conn()
	if conn == nil {
		return fmt.Errorf("peer %d has no connection: %w", peerID, syscall.ENOTCONN)
	}
	encoded := rpc.Encode()
	peer.writeMu.Lock()
	defer peer.writeMu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(p2pWriteTimeout))
	if _, err := conn.Write(encoded); err != nil {
		return fmt.Errorf("send to peer %d failed: %v: %w", peerID, err, syscall.EPIPE)
	}
	return nil
}

// Peers returns the current peer map.
func (t *TCPTransport) Peers() *PeerMap {
	return t.peerMap
}

// Addr returns the listen address.
func (t *TCPTransport) Addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.listenAddr
}

// Close shuts down the transport and all connections.
func (t *TCPTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.done)
	t.mu.Unlock()

	if t.ln != nil {
		t.ln.Close()
	}

	// 🛡️ Close each live connection exactly once. t.conns is the
	// authoritative set of every open socket (accepted + dialed). Peer
	// connections in peerMap are the same net.Conn objects, so closing
	// them again here would double-close; the peer conn refs are instead
	// nulled by cleanupConn once each conn's read loop exits (fix #668).
	t.mu.Lock()
	for conn := range t.conns {
		conn.Close()
	}
	t.mu.Unlock()

	t.wg.Wait()
	return nil
}
