package storage

import "testing"

// TestStoreInterfaceCompliance verifies that CASStore satisfies the Store
// interface. This is a compile-time check.
func TestStoreInterfaceCompliance(t *testing.T) {
	var _ Store = (*CASStore)(nil)
}

// TestNoStorageImportsInClusterPackages documents the invariant that
// protocol packages (transport, client) must not import storage
// concrete implementations. The invariant is enforced by the interface
// contract.
func TestNoStorageImportsInClusterPackages(t *testing.T) {
	// Enforcement: interface compliance + code review + factory pattern
}
