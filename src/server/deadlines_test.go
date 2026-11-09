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

// failingDeadlineComm simulates a transport that cannot apply a deadline.
type failingDeadlineComm struct {
	closed   int
	setCalls int
	fail     bool
}

func (f *failingDeadlineComm) SetAbsoluteDeadline(interface{}) error {
	f.setCalls++
	if f.fail {
		return errDeadline
	}
	return nil
}

func (f *failingDeadlineComm) Close() error {
	f.closed++
	return nil
}

var errDeadline = deadlineErr{}

type deadlineErr struct{}

func (deadlineErr) Error() string { return "deadline unsupported" }

func TestBeginPhaseAppliesDeadline(t *testing.T) {
	comm := &failingDeadlineComm{}
	if !beginPhase(comm, "127.0.0.1:1", "handshake", time.Second) {
		t.Fatal("expected the phase to start")
	}
	if comm.setCalls != 1 || comm.closed != 0 {
		t.Fatalf("setCalls=%d closed=%d", comm.setCalls, comm.closed)
	}
}

func TestBeginPhaseClosesOnDeadlineFailure(t *testing.T) {
	comm := &failingDeadlineComm{fail: true}
	if beginPhase(comm, "127.0.0.1:1", "metadata", time.Second) {
		t.Fatal("a failed deadline must not start the phase")
	}
	if comm.closed != 1 {
		t.Fatalf("closed=%d, want 1 (no unbounded read)", comm.closed)
	}
}
