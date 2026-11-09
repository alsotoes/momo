package server

import (
	"time"

	"github.com/alsotoes/momo/src/common"
)

// Network deadlines used to be hardcoded (#1149 Phase 4). They are now
// configurable, but a zero value — an older config, or a test that constructs
// common.Configuration directly — keeps the historical default rather than
// producing an immediate timeout.
const (
	defaultHandshakeTimeout         = 10 * time.Second
	defaultMetadataDeadline         = 60 * time.Second
	defaultPropagationTimeout       = 11 * time.Second
	defaultMaxConcurrentConnections = 1000
)

// handshakeTimeout returns the configured handshake deadline or the default.
func handshakeTimeout(cfg common.Configuration) time.Duration {
	if cfg.Server.HandshakeTimeout > 0 {
		return time.Duration(cfg.Server.HandshakeTimeout) * time.Second
	}
	return defaultHandshakeTimeout
}

// metadataDeadline returns the configured post-handshake metadata deadline.
func metadataDeadline(cfg common.Configuration) time.Duration {
	if cfg.Server.MetadataDeadline > 0 {
		return time.Duration(cfg.Server.MetadataDeadline) * time.Second
	}
	return defaultMetadataDeadline
}

// propagationTimeout returns how long a mode broadcast waits for an ack.
func propagationTimeout(cfg common.Configuration) time.Duration {
	if cfg.Coordination.PropagationTimeout > 0 {
		return time.Duration(cfg.Coordination.PropagationTimeout) * time.Second
	}
	return defaultPropagationTimeout
}

// maxConcurrentConnections bounds simultaneous replication connections.
func maxConcurrentConnections(cfg common.Configuration) int {
	if cfg.Server.MaxConcurrentConnections > 0 {
		return cfg.Server.MaxConcurrentConnections
	}
	return defaultMaxConcurrentConnections
}
