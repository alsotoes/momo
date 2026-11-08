package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

type recordingPublisher struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *recordingPublisher) PublishMetadata(name, hash string, size int64, remotePath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	return r.err
}

func TestPublishMetadataDisabledIsNoOp(t *testing.T) {
	setMetadataPublisher(nil)
	defer setMetadataPublisher(nil)
	if err := publishMetadataToQuorum("obj", "h", 1, ""); err != nil {
		t.Fatalf("disabled publisher must be a no-op: %v", err)
	}
}

func TestPublishMetadataPropagatesFailure(t *testing.T) {
	rec := &recordingPublisher{err: errors.New("quorum lost")}
	setMetadataPublisher(rec)
	defer setMetadataPublisher(nil)
	err := publishMetadataToQuorum("obj", "h", 1, "")
	if err == nil {
		t.Fatal("publisher failure must propagate")
	}
}

func TestInstallQuorumMetadataPublishGate(t *testing.T) {
	defer setMetadataPublisher(nil)
	provider := p2p.NewMetadataRPCProvider(0, nil, p2p.NewRing([]int32{0, 1}, 2), nil)

	cases := []struct {
		name     string
		cfg      common.Configuration
		provider *p2p.MetadataRPCProvider
		mFactor  int
		enabled  bool
	}{
		{"disabled by config", common.Configuration{}, provider, 3, false},
		{"no provider", common.Configuration{Momofs: common.ConfigurationMomofs{Enabled: true}}, nil, 3, false},
		{"single replica", common.Configuration{Momofs: common.ConfigurationMomofs{Enabled: true}}, provider, 1, false},
		{"enabled", common.Configuration{Momofs: common.ConfigurationMomofs{Enabled: true}}, provider, 3, true},
	}
	for _, tc := range cases {
		installQuorumMetadataPublish(tc.cfg, tc.provider, tc.mFactor)
		metadataPublishMu.RLock()
		active := metadataPublisherActive != nil
		metadataPublishMu.RUnlock()
		if active != tc.enabled {
			t.Fatalf("%s: enabled=%v, want %v", tc.name, active, tc.enabled)
		}
	}
	if err := publishMetadataToQuorum("obj", "h", 1, ""); err == nil {
		t.Fatal("the enabled case must reach the provider (nil store fails closed)")
	}
}

func TestGetFilePublishesMetadataToQuorum(t *testing.T) {
	rec := &recordingPublisher{}
	setMetadataPublisher(rec)
	defer setMetadataPublisher(nil)

	store, comm, content := newGetFileFixture(t)
	hash := sha256.Sum256([]byte(content))
	expected := hex.EncodeToString(hash[:])

	if err := getFile(comm, store, "published.txt", expected, int64(len(content)), ""); err != nil {
		t.Fatalf("getFile: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 1 || rec.calls[0] != "published.txt" {
		t.Fatalf("expected one publish for the object, got %v", rec.calls)
	}
}

func TestGetFileFailsWhenQuorumUnreachable(t *testing.T) {
	setMetadataPublisher(&recordingPublisher{err: syscall.EIO})
	defer setMetadataPublisher(nil)

	store, comm, content := newGetFileFixture(t)
	hash := sha256.Sum256([]byte(content))
	if err := getFile(comm, store, "fail.txt", hex.EncodeToString(hash[:]), int64(len(content)), ""); err == nil {
		t.Fatal("a failed quorum write must fail the request so the client retries")
	}
}

// newGetFileFixture builds a CAS store plus a pipe that streams the content.
func newGetFileFixture(t *testing.T) (*storage.CASStore, transport.Communicator, string) {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, client := net.Pipe()
	content := "metadata quorum payload"
	go func() {
		defer func() { _ = client.Close() }()
		_, _ = client.Write([]byte(content))
	}()
	t.Cleanup(func() { _ = server.Close() })
	return store, transport.NewMomoTCPCommunicator(server), content
}
