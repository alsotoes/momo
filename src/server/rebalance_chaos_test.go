package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// chaosWorld is an in-process cluster: every node owns a real CAS store and a
// pusher that "delivers" bytes by copying them into the target's store. The
// copy happens before ack, mirroring the real push contract.
type chaosWorld struct {
	t     *testing.T
	nodes []*storage.CASStore
	cmap  *common.ClusterMap
	seen  map[string]bool
}

func newChaosWorld(t *testing.T, n int) *chaosWorld {
	t.Helper()
	w := &chaosWorld{t: t, seen: map[string]bool{}}
	var nodes []*common.Node
	for i := 0; i < n; i++ {
		store, err := storage.NewCASStore(t.TempDir())
		if err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
		t.Cleanup(func() { _ = store.Close() })
		w.nodes = append(w.nodes, store)
		nodes = append(nodes, &common.Node{ID: i, Weight: 1, Domain: fmt.Sprintf("d%d", i%2)})
	}
	w.cmap = &common.ClusterMap{Nodes: nodes}
	return w
}

// put writes one object to a node and records its hash.
func (w *chaosWorld) put(node int, name string, payload []byte) string {
	w.t.Helper()
	hash := common.HashBytes(payload)
	if err := w.nodes[node].Put(name, hash, int64(len(payload)), "", bytes.NewReader(payload)); err != nil {
		w.t.Fatalf("put %s: %v", name, err)
	}
	w.seen[hash] = true
	return hash
}

// replicas counts nodes that hold the blob bytes.
func (w *chaosWorld) replicas(hash string) int {
	w.t.Helper()
	count := 0
	for _, node := range w.nodes {
		blobs, err := node.ListBlobs()
		if err != nil {
			w.t.Fatalf("ListBlobs: %v", err)
		}
		for _, b := range blobs {
			if b.Hash == hash {
				count++
				break
			}
		}
	}
	return count
}

// assertNoLoss is the safety invariant: shedding may reduce copies but never to
// zero.
func (w *chaosWorld) assertNoLoss(step string) {
	w.t.Helper()
	for hash := range w.seen {
		if got := w.replicas(hash); got < 1 {
			w.t.Fatalf("step %s: blob %s has zero copies", step, hash)
		}
	}
}

// chaosPusher copies bytes between stores; failNext simulates an unreachable peer.
type chaosPusher struct {
	world *chaosWorld
	from  int
	// failRate in percent, driven by the test's seeded RNG.
	rng      *rand.Rand
	failRate int
}

func (p *chaosPusher) Push(_ context.Context, hash string, targetID int) error {
	if p.failRate > 0 && p.rng.Intn(100) < p.failRate {
		return fmt.Errorf("simulated unreachable peer %d", targetID)
	}
	rc, err := p.world.nodes[p.from].GetBlobByHash(hash)
	if err != nil {
		return err
	}
	data, rerr := io.ReadAll(rc)
	_ = rc.Close()
	if rerr != nil {
		return rerr
	}
	return p.world.nodes[targetID].Put(hash, hash, int64(len(data)), "", bytes.NewReader(data))
}

func (w *chaosWorld) runner(node int, pusher *chaosPusher) *rebalanceRunner {
	return &rebalanceRunner{
		store: w.nodes[node], pusher: pusher, cmap: w.cmap, self: node, rf: 3,
		cfg: common.ConfigurationRebalance{Enabled: true, Interval: 60, DiskHighWater: 0.0001, DiskLowWater: 0, MaxShedPerCycle: 2},
	}
}

// TestRebalanceChaosNeverLosesTheLastCopy drives randomized shed and join
// passes with simulated peer failures across a 5-node cluster and asserts the
// only invariant that matters: an object is never reduced to zero copies.
func TestRebalanceChaosNeverLosesTheLastCopy(t *testing.T) {
	const (
		nodes  = 5
		seeds  = 8
		rounds = 60
	)
	for seed := int64(1); seed <= seeds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w := newChaosWorld(t, nodes)

		// Seed the cluster: every object lands on every node, so sheds always
		// have a peer that already holds the bytes.
		for i := 0; i < 6; i++ {
			payload := []byte(fmt.Sprintf("chaos-object-%d-%d", seed, i))
			for n := 0; n < nodes; n++ {
				w.put(n, fmt.Sprintf("obj-%d", i), payload)
			}
		}

		for round := 0; round < rounds; round++ {
			node := rng.Intn(nodes)
			pusher := &chaosPusher{world: w, from: node, rng: rng, failRate: 30}
			r := w.runner(node, pusher)
			switch rng.Intn(2) {
			case 0:
				if _, err := r.rebalanceCycleForTest(context.Background()); err != nil {
					t.Fatalf("seed %d round %d: shed: %v", seed, round, err)
				}
			case 1:
				peer := (node + 1 + rng.Intn(nodes-1)) % nodes
				_ = r.onPeerJoin(context.Background(), peer)
			}
			w.assertNoLoss(fmt.Sprintf("seed=%d round=%d", seed, round))
		}
	}
}
