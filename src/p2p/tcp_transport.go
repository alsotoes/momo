package p2p

import (
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
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
		t.conns[conn] = struct{}{}
		t.mu.Unlock()

		t.wg.Add(1)
		go t.handleConn(conn)
	}
}

// handleConn reads RPCs from a single connection and delivers them to rpcCh.
// The peer ID is extracted from the first RPC received.
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer t.wg.Done()
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("P2P handleConn panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()

	var peer *Peer
	var peerID int32 = -1

	for {
		rpc, err := DecodeRPC(conn)
		if err != nil {
			select {
			case <-t.done:
				return
			default:
			}
			if peerID >= 0 {
				log.Printf("P2P peer %d disconnected: %v (errno=%d)", peerID, err, syscall.ECONNRESET)
			}
			return
		}

		if peer == nil {
			peerID = rpc.From
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

// Dial connects to a peer at the given address.
// If a peer with the same ID already exists, it returns the existing peer.
func (t *TCPTransport) Dial(id int32, addr string) (*Peer, error) {
	if existing := t.peerMap.Get(id); existing != nil {
		return existing, nil
	}

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("p2p dial %s failed: %v: %w", addr, err, syscall.ECONNREFUSED)
	}

	t.mu.Lock()
	t.conns[conn] = struct{}{}
	t.mu.Unlock()

	peer := NewPeer(id, addr)
	peer.SetConn(conn)
	t.peerMap.Add(peer)

	t.wg.Add(1)
	go t.readLoop(id, conn)

	log.Printf("P2P dialed peer %d at %s", id, addr)
	return peer, nil
}

// readLoop reads RPCs from a dialed connection.
func (t *TCPTransport) readLoop(peerID int32, conn net.Conn) {
	defer t.wg.Done()
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("P2P readLoop panic recovered for peer %d: %v (errno=%d)", peerID, r, syscall.EIO)
		}
	}()

	for {
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
func (t *TCPTransport) Broadcast(rpc *RPC) int {
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
		if _, err := conn.Write(encoded); err != nil {
			log.Printf("P2P broadcast to peer %d failed: %v (errno=%d)", p.ID, err, syscall.EPIPE)
			continue
		}
		sent++
	}
	return sent
}

// Send sends an RPC to a specific peer by ID.
func (t *TCPTransport) Send(peerID int32, rpc *RPC) error {
	peer := t.peerMap.Get(peerID)
	if peer == nil {
		return fmt.Errorf("peer %d not found: %w", peerID, syscall.ENOENT)
	}
	conn := peer.Conn()
	if conn == nil {
		return fmt.Errorf("peer %d has no connection: %w", peerID, syscall.ENOTCONN)
	}
	encoded := rpc.Encode()
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

	t.mu.Lock()
	for conn := range t.conns {
		conn.Close()
	}
	t.mu.Unlock()

	for _, p := range t.peerMap.All() {
		if conn := p.Conn(); conn != nil {
			conn.Close()
		}
	}

	t.wg.Wait()
	return nil
}
