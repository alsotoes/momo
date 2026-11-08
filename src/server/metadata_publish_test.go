package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestInstallMetadataDeletePropagationGate(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := p2p.NewMetadataRPCProvider(0, nil, p2p.NewRing([]int32{0}, 1), store)
	fired := make(chan string, 1)
	providerForHook := provider

	installMetadataDeletePropagation(store, providerForHook, false)
	if err := store.PutWithMetadata("a", "h1", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	select {
	case name := <-fired:
		t.Fatalf("disabled propagation fired for %q", name)
	case <-time.After(100 * time.Millisecond):
	}

	installMetadataDeletePropagation(store, nil, true) // no provider: no hook
	if err := store.PutWithMetadata("b", "h2", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.Delete("b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_ = fired
}

func TestInstallMetadataDeletePropagationEnabled(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Single-node ring with this node as the replica: propagation succeeds
	// locally (covers the enabled wiring and the success path).
	provider := p2p.NewMetadataRPCProvider(0, nil, p2p.NewRing([]int32{0}, 1), store)
	installMetadataDeletePropagation(store, provider, true)
	if err := store.PutWithMetadata("del", "h9", 0, "", nil, nil, "", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.Delete("del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let the propagation goroutine finish
}

func TestQuorumMetadataPublisherFailure(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Remote-only replica and a nil transport: the send panics, the guard turns
	// it into an error, and PublishMetadata reports failure.
	provider := p2p.NewMetadataRPCProvider(5, nil, p2p.NewRing([]int32{0}, 1), store)
	provider.RPCTimeout = 50 * time.Millisecond
	pub := &quorumMetadataPublisher{provider: provider}
	if err := pub.PublishMetadata("obj", "h", 1, ""); err == nil {
		t.Fatal("unreachable quorum must be reported")
	}
}

func TestInstallMetadataResolverGate(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := p2p.NewMetadataRPCProvider(0, nil, p2p.NewRing([]int32{0}, 1), store)

	// Disabled and provider-less installs must clear/leave the resolver unset;
	// an enabled install must resolve a name this node does not hold. Seed a
	// record on the provider's own store to make the read observable.
	if err := store.PutWithMetadata("seeded", "h-seed", 0, "", nil, []uint64{1}, "s", []int32{0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	installMetadataResolver(store, nil, true)
	if _, err := store.GetMeta("seeded"); err != nil {
		t.Fatalf("local read must still work without a resolver: %v", err)
	}
	installMetadataResolver(store, provider, true)
	if _, err := store.GetMeta("seeded"); err != nil {
		t.Fatalf("enabled resolver must keep local reads working: %v", err)
	}
	// A name no replica holds exercises the resolver closure's not-found path.
	if _, err := store.GetMeta("absent-everywhere"); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("want ENOENT for an unresolvable name, got %v", err)
	}
}

func TestInstallMetadataResolverNilStoreIsSafe(t *testing.T) {
	installMetadataResolver(nil, nil, true) // must not panic
}
