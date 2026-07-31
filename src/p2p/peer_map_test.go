package p2p

import (
	"sync"
	"testing"
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
