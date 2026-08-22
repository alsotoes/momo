package p2p

import (
	"sync"
	"testing"
	"time"
)

func TestPeerMap_AddGetRemove(t *testing.T) {
	m := NewPeerMap()

	p1 := NewPeer(1, "127.0.0.1:4450")
	p2 := NewPeer(2, "127.0.0.2:4450")

	m.Add(p1)
	m.Add(p2)

	if m.Count() != 2 {
		t.Errorf("expected count 2, got %d", m.Count())
	}

	if m.Get(1) != p1 {
		t.Error("Get(1) returned wrong peer")
	}
	if m.Get(2) != p2 {
		t.Error("Get(2) returned wrong peer")
	}
	if m.Get(99) != nil {
		t.Error("Get(99) should return nil")
	}

	m.Remove(1)
	if m.Count() != 1 {
		t.Errorf("expected count 1 after remove, got %d", m.Count())
	}
	if m.Get(1) != nil {
		t.Error("Get(1) should return nil after remove")
	}
}

func TestPeerMap_All(t *testing.T) {
	m := NewPeerMap()
	m.Add(NewPeer(1, "a"))
	m.Add(NewPeer(2, "b"))
	m.Add(NewPeer(3, "c"))

	all := m.All()
	if len(all) != 3 {
		t.Errorf("expected 3 peers, got %d", len(all))
	}
}

func TestPeerMap_Alive(t *testing.T) {
	m := NewPeerMap()
	p1 := NewPeer(1, "a")
	p2 := NewPeer(2, "b")
	p2.SetState(PeerStateSuspect)

	m.Add(p1)
	m.Add(p2)

	alive := m.Alive()
	if len(alive) != 1 {
		t.Errorf("expected 1 alive peer, got %d", len(alive))
	}
	if alive[0].ID != 1 {
		t.Errorf("expected peer 1, got %d", alive[0].ID)
	}
}

func TestPeerRRT(t *testing.T) {
	p := NewPeer(1, "addr")
	if p.RTT() != 0 {
		t.Fatalf("expected initial RTT 0, got %v", p.RTT())
	}
	d := 15 * time.Millisecond
	p.SetRTT(d)
	if got := p.RTT(); got != d {
		t.Fatalf("expected RTT %v, got %v", d, got)
	}
	// SetRTT(0) clears back to unknown.
	p.SetRTT(0)
	if p.RTT() != 0 {
		t.Fatalf("expected RTT cleared to 0, got %v", p.RTT())
	}
}

func TestPeerMap_AliveByQuality(t *testing.T) {
	m := NewPeerMap()
	a := NewPeer(1, "a")
	b := NewPeer(2, "b")
	c := NewPeer(3, "c")
	d := NewPeer(4, "d")
	e := NewPeer(5, "e")
	f := NewPeer(6, "f")

	a.SetRTT(5 * time.Millisecond)
	b.SetRTT(50 * time.Millisecond)
	c.SetRTT(2 * time.Millisecond)
	// d, e: unknown RTT (0)
	e.SetState(PeerStateSuspect)
	f.SetState(PeerStateOffline)

	m.Add(a)
	m.Add(b)
	m.Add(c)
	m.Add(d)
	m.Add(e)
	m.Add(f)

	got := m.AliveByQuality()
	// Expect c(2ms), a(5ms), b(50ms), d(unknown) — suspect e and offline f excluded.
	if len(got) != 4 {
		t.Fatalf("expected 4 peers, got %d: %+v", len(got), got)
	}
	wantIDs := []int32{3, 1, 2, 4}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("expected order [%v] index %d = %d, got %d", wantIDs, i, want, got[i].ID)
		}
	}
}

func TestPeerMap_AliveByQuality_StableWhenAllUnknown(t *testing.T) {
	m := NewPeerMap()
	m.Add(NewPeer(1, "a"))
	m.Add(NewPeer(2, "b"))
	m.Add(NewPeer(3, "c"))

	got := m.AliveByQuality()
	if len(got) != 3 {
		t.Fatalf("expected all 3 alive peers, got %d", len(got))
	}
	// All alive peers preserved regardless of ordering.
	seen := map[int32]bool{}
	for _, p := range got {
		seen[p.ID] = true
	}
	for id := int32(1); id <= 3; id++ {
		if !seen[id] {
			t.Fatalf("peer %d missing from quality set", id)
		}
	}
}

func TestPeerMap_AliveCount(t *testing.T) {
	m := NewPeerMap()
	m.Add(NewPeer(1, "a"))
	m.Add(NewPeer(2, "b"))
	m.Add(NewPeer(3, "c"))
	m.Get(2).SetState(PeerStateSuspect)
	m.Get(3).SetState(PeerStateOffline)

	if got := m.AliveCount(); got != 1 {
		t.Fatalf("expected AliveCount 1 (only peer 1 alive), got %d", got)
	}
}

func TestPeerMap_RngInitialized(t *testing.T) {
	// The per-instance RNG is the concurrency-safe selection source; ensure it
	// is always constructed (never nil) and distinct per PeerMap instance.
	a := NewPeerMap()
	b := NewPeerMap()
	if a.rng == nil {
		t.Fatal("expected PeerMap a to have an initialized rng")
	}
	if b.rng == nil {
		t.Fatal("expected PeerMap b to have an initialized rng")
	}
	if a.rng == b.rng {
		t.Fatal("expected independent rng instances per PeerMap")
	}
}

func TestPeerMap_RandomPeers(t *testing.T) {
	m := NewPeerMap()
	for i := int32(1); i <= 10; i++ {
		m.Add(NewPeer(i, "addr"))
	}

	result := m.RandomPeers(3, 5)
	if len(result) > 3 {
		t.Errorf("expected at most 3 peers, got %d", len(result))
	}
	for _, p := range result {
		if p.ID == 5 {
			t.Error("excluded peer 5 was returned")
		}
	}
}

func TestPeerMap_PeerInfos(t *testing.T) {
	m := NewPeerMap()
	m.Add(NewPeer(1, "127.0.0.1:4450"))
	m.Add(NewPeer(2, "127.0.0.2:4450"))

	infos := m.PeerInfos()
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}
}

func TestPeerMap_ConcurrentAccess(t *testing.T) {
	m := NewPeerMap()
	var wg sync.WaitGroup

	for i := int32(0); i < 100; i++ {
		wg.Add(1)
		go func(id int32) {
			defer wg.Done()
			m.Add(NewPeer(id, "addr"))
		}(i)
	}

	wg.Wait()

	if m.Count() != 100 {
		t.Errorf("expected 100 peers, got %d", m.Count())
	}
}
