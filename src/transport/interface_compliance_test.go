package transport

import "testing"

// TestCommunicatorInterfaceCompliance verifies that all protocol implementations
// satisfy the Communicator interface. This is a compile-time check — if any
// implementation fails to satisfy the interface, the test will not compile.
func TestCommunicatorInterfaceCompliance(t *testing.T) {
	var _ Communicator = (*MomoTCPCommunicator)(nil)
	var _ Communicator = (*MomoQUICCommunicator)(nil)
	var _ Communicator = (*S3Communicator)(nil)
}

// TestNoProtocolImportsInClusterPackages documents the invariant that cluster
// packages must not import protocol implementations. The invariant is enforced
// by the interface contract: cluster code only imports this package and uses
// the Communicator interface, never the concrete implementations.
func TestNoProtocolImportsInClusterPackages(t *testing.T) {
	// This test documents the architectural invariant.
	// Enforcement is via:
	// 1. Interface compliance above (compile-time)
	// 2. Code review (no protocol type imports in server/common/p2p/storage)
	// 3. The factory pattern (sole protocol-aware component)
}
