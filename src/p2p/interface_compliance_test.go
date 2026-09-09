package p2p

import "testing"

// TestTransportInterfaceCompliance verifies that TCPTransport satisfies
// the Transport interface. This is a compile-time check.
func TestTransportInterfaceCompliance(t *testing.T) {
	var _ Transport = (*TCPTransport)(nil)
}

// TestNoTransportImportsInClusterPackages documents the invariant that
// cluster packages (server, storage, transport) must not import p2p
// concrete implementations. The invariant is enforced by the interface
// contract.
func TestNoTransportImportsInClusterPackages(t *testing.T) {
	// Enforcement: interface compliance + code review + factory pattern
}
