package client

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

func TestPheromone_InitialState(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()
	p := router.GetPheromone(1)
	if p != InitialPheromone {
		t.Fatalf("expected initial pheromone %f, got %f", InitialPheromone, p)
	}
}

func TestPheromone_RecordSuccessAndPenalty(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()

	// Fast RTT increases pheromone
	router.RecordSuccess(1, 10*time.Millisecond)
	p1 := router.GetPheromone(1)
	if p1 <= InitialPheromone {
		t.Fatalf("expected increased pheromone after fast success, got %f", p1)
	}

	// Repeated successes saturate at MaxPheromone
	for range 10 {
		router.RecordSuccess(1, 5*time.Millisecond)
	}
	if pMax := router.GetPheromone(1); pMax > MaxPheromone {
		t.Fatalf("expected bounded at MaxPheromone (%f), got %f", MaxPheromone, pMax)
	}

	// Failure aggressively reduces pheromone
	router.RecordFailure(1)
	pPenalized := router.GetPheromone(1)
	if pPenalized >= p1 {
		t.Fatalf("expected penalty to decrease pheromone, got %f", pPenalized)
	}

	// Multiple failures bounded by MinPheromone
	for range 10 {
		router.RecordFailure(1)
	}
	if pFloor := router.GetPheromone(1); pFloor < MinPheromone {
		t.Fatalf("expected floor at MinPheromone (%f), got %f", MinPheromone, pFloor)
	}
}

func TestPheromone_Evaporation(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()
	router.RecordSuccess(1, 10*time.Millisecond)
	before := router.GetPheromone(1)

	router.Evaporate()
	after := router.GetPheromone(1)

	expected := before * EvaporationFactor
	if math.Abs(after-expected) > 0.001 {
		t.Fatalf("expected evaporated value %f, got %f", expected, after)
	}
}

func TestPheromone_SelectReplicaDistribution(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()

	nodeA := &common.Node{ID: 0, Addr: "127.0.0.1:8000"}
	nodeB := &common.Node{ID: 1, Addr: "127.0.0.1:8001"}

	// Give node A high reputation, node B lowest reputation
	for range 5 {
		router.RecordSuccess(0, 10*time.Millisecond)
		router.RecordFailure(1)
	}

	pA := router.GetPheromone(0)
	pB := router.GetPheromone(1)

	candidates := []*common.Node{nodeA, nodeB}
	counts := make(map[int]int)

	const trials = 1000
	for range trials {
		chosen, err := router.SelectReplica(candidates)
		if err != nil {
			t.Fatalf("SelectReplica failed: %v", err)
		}
		counts[chosen.ID]++
	}

	ratioExpected := pA / (pA + pB)
	ratioActual := float64(counts[0]) / float64(trials)

	// Actual distribution should be within ±10% of theoretical roulette wheel expectation
	if math.Abs(ratioActual-ratioExpected) > 0.10 {
		t.Fatalf("expected selection ratio ~%f, got %f (counts: A=%d, B=%d)", ratioExpected, ratioActual, counts[0], counts[1])
	}
}

func TestPheromone_PrioritizeReplicas(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()

	nodeA := &common.Node{ID: 0, Addr: "127.0.0.1:8000"}
	nodeB := &common.Node{ID: 1, Addr: "127.0.0.1:8001"}
	nodeC := &common.Node{ID: 2, Addr: "127.0.0.1:8002"}

	// Set A > B > C
	for range 5 {
		router.RecordSuccess(0, 5*time.Millisecond)
	}
	router.RecordSuccess(1, 100*time.Millisecond)
	for range 5 {
		router.RecordFailure(2)
	}

	candidates := []*common.Node{nodeA, nodeB, nodeC}
	ordered, err := router.PrioritizeReplicas(candidates)
	if err != nil {
		t.Fatalf("PrioritizeReplicas failed: %v", err)
	}

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ordered nodes, got %d", len(ordered))
	}
}

func TestPheromone_ConcurrentAccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	router := NewPheromoneRouter()
	candidates := []*common.Node{
		{ID: 0, Addr: "127.0.0.1:8000"},
		{ID: 1, Addr: "127.0.0.1:8001"},
		{ID: 2, Addr: "127.0.0.1:8002"},
	}

	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 100

	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				if (id+j)%3 == 0 {
					router.RecordSuccess(id%3, time.Duration(10+j)*time.Millisecond)
				} else if (id+j)%3 == 1 {
					router.RecordFailure(id % 3)
				} else {
					_, _ = router.SelectReplica(candidates)
				}
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkPheromoneSelectReplica(b *testing.B) {
	router := NewPheromoneRouter()
	candidates := []*common.Node{
		{ID: 0, Addr: "127.0.0.1:8000"},
		{ID: 1, Addr: "127.0.0.1:8001"},
		{ID: 2, Addr: "127.0.0.1:8002"},
	}
	router.RecordSuccess(0, 10*time.Millisecond)
	router.RecordSuccess(1, 25*time.Millisecond)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = router.SelectReplica(candidates)
	}
}
