package server

import (
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func TestDowngradeToServerSideMode(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name             string
		currentMode      int
		replicationOrder []int
		clientSideModes  []int
		expected         int
	}{
		{
			name:             "primary-splay downgrades to splay",
			currentMode:      3,
			replicationOrder: []int{3, 2, 1},
			clientSideModes:  []int{3},
			expected:         2,
		},
		{
			name:             "splay not downgraded (not in client-side list)",
			currentMode:      2,
			replicationOrder: []int{3, 2, 1},
			clientSideModes:  []int{3},
			expected:         2,
		},
		{
			name:             "chain not downgraded (not in client-side list)",
			currentMode:      1,
			replicationOrder: []int{3, 2, 1},
			clientSideModes:  []int{3},
			expected:         1,
		},
		{
			name:             "future mode 4 and 3 both in client-side list",
			currentMode:      4,
			replicationOrder: []int{4, 3, 2, 1},
			clientSideModes:  []int{3, 4},
			expected:         2,
		},
		{
			name:             "mode 3 downgrades with future mode 4 also client-side",
			currentMode:      3,
			replicationOrder: []int{4, 3, 2, 1},
			clientSideModes:  []int{3, 4},
			expected:         2,
		},
		{
			name:             "all modes are client-side — fallback to ReplicationNone",
			currentMode:      3,
			replicationOrder: []int{3, 2, 1},
			clientSideModes:  []int{3, 2, 1},
			expected:         common.ReplicationNone,
		},
		{
			name:             "current mode not in replication order and not client-side — returned as-is",
			currentMode:      5,
			replicationOrder: []int{3, 2, 1},
			clientSideModes:  []int{3},
			expected:         5,
		},
		{
			name:             "empty replication order — fallback to ReplicationNone",
			currentMode:      3,
			replicationOrder: []int{},
			clientSideModes:  []int{3},
			expected:         common.ReplicationNone,
		},
		{
			name:             "single server-side mode available",
			currentMode:      3,
			replicationOrder: []int{3, 1},
			clientSideModes:  []int{3},
			expected:         1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downgradeToServerSideMode(tt.currentMode, tt.replicationOrder, tt.clientSideModes)
			if result != tt.expected {
				t.Errorf("downgradeToServerSideMode(%d, %v, %v) = %d, want %d",
					tt.currentMode, tt.replicationOrder, tt.clientSideModes, result, tt.expected)
			}
		})
	}
}

func TestDowngradeToServerSideMode_DefaultConfig(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Default config: replication_order=3,2,1, client_side_replication_modes=3
	// Mode 3 (primary-splay) should downgrade to 2 (splay)
	result := downgradeToServerSideMode(3, []int{3, 2, 1}, []int{3})
	if result != 2 {
		t.Errorf("Expected downgrade to mode 2 (splay), got %d", result)
	}

	// Mode 2 (splay) should not be downgraded
	result = downgradeToServerSideMode(2, []int{3, 2, 1}, []int{3})
	if result != 2 {
		t.Errorf("Expected mode 2 (splay) to stay, got %d", result)
	}

	// Mode 1 (chain) should not be downgraded
	result = downgradeToServerSideMode(1, []int{3, 2, 1}, []int{3})
	if result != 1 {
		t.Errorf("Expected mode 1 (chain) to stay, got %d", result)
	}
}
