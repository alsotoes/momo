package common

import (
	"testing"
)

func BenchmarkClusterMap_Placement(b *testing.B) {
	nodes := []*Node{
		{ID: 0, Weight: 1, Addr: "127.0.0.1:4440"},
		{ID: 1, Weight: 1, Addr: "127.0.0.1:4441"},
		{ID: 2, Weight: 1, Addr: "127.0.0.1:4442"},
	}
	m := &ClusterMap{Nodes: nodes}
	objectHash := "eb0e30ff02be45f64a19881497f0f4233a9cfb674243e652d6299bf176551897"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Placement(objectHash, 2)
	}
}
