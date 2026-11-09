package server

import (
	"log"
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

// deadlineSetter is the narrow communicator surface beginPhase needs.
type deadlineSetter interface {
	SetAbsoluteDeadline(t interface{}) error
	Close() error
}

// beginPhase applies an absolute deadline for one protocol phase. When the
// deadline cannot be applied the connection is closed and the function reports
// false: continuing would leave an unbounded read, which is exactly the
// slowloris condition these deadlines exist to prevent.
func beginPhase(comm deadlineSetter, remoteAddr, phase string, d time.Duration) bool {
	if err := comm.SetAbsoluteDeadline(time.Now().Add(d)); err != nil {
		log.Printf("AUDIT: Failed to set %s deadline for %s: %v", phase, common.SanitizeLog(remoteAddr), err)
		if cerr := comm.Close(); cerr != nil {
			log.Printf("AUDIT: Failed to close connection for %s: %v", common.SanitizeLog(remoteAddr), cerr)
		}
		return false
	}
	return true
}
