package p2p

import (
	"net"
)

// Transport defines the interface for P2P network communication.
// It manages peer connections, message dispatch, and lifecycle.
type Transport interface {
	// Listen starts accepting connections on the given address.
	Listen(addr string) error

	// Dial connects to a peer at the given address and returns the peer.
	Dial(id int32, addr string) (*Peer, error)

	// Consume returns the channel of incoming RPCs from all peers.
	Consume() <-chan RPC

	// Broadcast sends an RPC to all active peers.
	Broadcast(rpc *RPC) int

	// Send sends an RPC to a specific peer by ID.
	Send(peerID int32, rpc *RPC) error

	// Peers returns the current peer map.
	Peers() *PeerMap

	// Close shuts down the transport and all connections.
	Close() error

	// Addr returns the listen address, or empty if not listening.
	Addr() string
}

// TCPTransportConfig holds configuration for TCPTransport.
type TCPTransportConfig struct {
	LocalID  int32
	AuthFunc func(id int32) bool
}

// Ensure net.Listener is referenced for interface satisfaction checks.
var _ net.Listener = (*net.TCPListener)(nil)
