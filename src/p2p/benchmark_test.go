package p2p

import (
	"testing"
)

func BenchmarkRPC_Encode(b *testing.B) {
	rpc := &RPC{
		From: 1,
		Type: MsgHeartbeat,
		Payload: (&HeartbeatPayload{
			Peers: []PeerInfo{
				{ID: 1, Addr: "127.0.0.1:4450"},
				{ID: 2, Addr: "127.0.0.2:4450"},
				{ID: 3, Addr: "127.0.0.3:4450"},
			},
		}).Encode(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rpc.Encode()
	}
}

func BenchmarkHeartbeatPayload_Encode(b *testing.B) {
	h := &HeartbeatPayload{
		Peers: []PeerInfo{
			{ID: 1, Addr: "127.0.0.1:4450"},
			{ID: 2, Addr: "127.0.0.2:4450"},
			{ID: 3, Addr: "127.0.0.3:4450"},
			{ID: 4, Addr: "127.0.0.4:4450"},
			{ID: 5, Addr: "127.0.0.5:4450"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Encode()
	}
}

func BenchmarkPeerMap_RandomPeers(b *testing.B) {
	m := NewPeerMap()
	for i := int32(1); i <= 100; i++ {
		m.Add(NewPeer(i, "addr"))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.RandomPeers(3, 0)
	}
}

func BenchmarkPeerMap_PeerInfos(b *testing.B) {
	m := NewPeerMap()
	for i := int32(1); i <= 100; i++ {
		m.Add(NewPeer(i, "addr"))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.PeerInfos()
	}
}
