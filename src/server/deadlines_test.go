package server

import (
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestDeadlineHelpersFallBackToHistoricalDefaults(t *testing.T) {
	cfg := common.Configuration{}
	if got := handshakeTimeout(cfg); got != 10*time.Second {
		t.Fatalf("handshakeTimeout = %v, want 10s", got)
	}
	if got := metadataDeadline(cfg); got != 60*time.Second {
		t.Fatalf("metadataDeadline = %v, want 60s", got)
	}
	if got := propagationTimeout(cfg); got != 11*time.Second {
		t.Fatalf("propagationTimeout = %v, want 11s", got)
	}
	if got := maxConcurrentConnections(cfg); got != 1000 {
		t.Fatalf("maxConcurrentConnections = %d, want 1000", got)
	}
}

func TestDeadlineHelpersUseConfiguredValues(t *testing.T) {
	cfg := common.Configuration{
		Server: common.ConfigurationServer{
			HandshakeTimeout:         3,
			MetadataDeadline:         120,
			MaxConcurrentConnections: 250,
		},
		Coordination: common.ConfigurationCoordination{PropagationTimeout: 4},
	}
	if got := handshakeTimeout(cfg); got != 3*time.Second {
		t.Fatalf("handshakeTimeout = %v", got)
	}
	if got := metadataDeadline(cfg); got != 120*time.Second {
		t.Fatalf("metadataDeadline = %v", got)
	}
	if got := propagationTimeout(cfg); got != 4*time.Second {
		t.Fatalf("propagationTimeout = %v", got)
	}
	if got := maxConcurrentConnections(cfg); got != 250 {
		t.Fatalf("maxConcurrentConnections = %d", got)
	}
}
