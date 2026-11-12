package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/shirou/gopsutil/v3/mem"
)

// MockSystemMetrics is a mock implementation of the SystemMetrics interface for testing.
type MockSystemMetrics struct {
	memStat *mem.VirtualMemoryStat
	cpuStat []float64
}

func (msm *MockSystemMetrics) VirtualMemory() (*mem.VirtualMemoryStat, error) {
	return msm.memStat, nil
}

func (msm *MockSystemMetrics) CPUPercent() ([]float64, error) {
	return msm.cpuStat, nil
}

func TestCheckMetricsAndSwap(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			PolymorphicSystem: true,
		},
		Metrics: common.ConfigurationMetrics{
			MinThreshold: 0.2,
			MaxThreshold: 0.8,
		},
	}

	replicationOrder := []int{1, 2, 3}

	tests := []struct {
		name            string
		currentIndex    int
		memUsedPercent  float64
		cpuUsedPercent  float64
		expectedIndex   int
		expectedChanged bool
	}{
		{
			name:            "Should not change when metrics are normal",
			currentIndex:    1, // Points to 2 in [1, 2, 3]
			memUsedPercent:  50.0,
			cpuUsedPercent:  50.0,
			expectedIndex:   1,
			expectedChanged: false,
		},
		{
			name:            "Should increase replication mode when usage is high",
			currentIndex:    1,
			memUsedPercent:  90.0,
			cpuUsedPercent:  50.0,
			expectedIndex:   2,
			expectedChanged: true,
		},
		{
			name:            "Should decrease replication mode when usage is low",
			currentIndex:    1,
			memUsedPercent:  10.0, // Triggers memFree <= minThreshold
			cpuUsedPercent:  10.0,
			expectedIndex:   0,
			expectedChanged: true,
		},
		{
			name:            "Should not increase replication mode at max level",
			currentIndex:    2,
			memUsedPercent:  90.0,
			cpuUsedPercent:  90.0,
			expectedIndex:   2,
			expectedChanged: false,
		},
		{
			name:            "Should not decrease replication mode at min level",
			currentIndex:    0,
			memUsedPercent:  10.0,
			cpuUsedPercent:  10.0,
			expectedIndex:   0,
			expectedChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &MockSystemMetrics{
				memStat: &mem.VirtualMemoryStat{
					UsedPercent: tt.memUsedPercent,
				},
				cpuStat: []float64{tt.cpuUsedPercent},
			}

			maxThreshPercent := cfg.Metrics.MaxThreshold * 100
			minThreshPercent := cfg.Metrics.MinThreshold * 100
			newIndex, changed := checkMetricsAndSwap(sm, tt.currentIndex, replicationOrder, maxThreshPercent, minThreshPercent)

			if newIndex != tt.expectedIndex {
				t.Errorf("Expected index %d, got %d", tt.expectedIndex, newIndex)
			}
			if changed != tt.expectedChanged {
				t.Errorf("Expected changed status %v, got %v", tt.expectedChanged, changed)
			}
		})
	}
}

func TestRealSystemMetrics(t *testing.T) {
	rsm := &RealSystemMetrics{}
	_, err := rsm.VirtualMemory()
	if err != nil {
		t.Errorf("Unexpected error from VirtualMemory: %v", err)
	}
	_, err = rsm.CPUPercent()
	if err != nil {
		t.Errorf("Unexpected error from CPUPercent: %v", err)
	}
}

func TestReplicationOrderTrackerObservesSwitch(t *testing.T) {
	// Enabled tracker: exercises observeMode's non-nil branch and step's
	// tracker refresh block (#1146).
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			ReplicationOrder: []int{common.ReplicationNone, common.ReplicationChain},
		},
		ReplicationOrder: common.ConfigurationReplicationOrder{
			Enabled:           true,
			ReevaluateSeconds: 60,
			Alpha:             0.2,
			MinSamples:        1,
			SuccessWeight:     0.4,
			LatencyWeight:     0.3,
			ThroughputWeight:  0.2,
			ErrorWeight:       0.1,
		},
	}
	tracker := newReplicationOrderTracker(cfg)
	if tracker == nil {
		t.Fatal("expected a tracker to be created")
	}

	c := &metricsController{
		cfg:              cfg,
		authToken:        []byte("token"),
		order:            []int{common.ReplicationNone, common.ReplicationChain},
		store:            nil,
		sm:               &MockSystemMetrics{memStat: &mem.VirtualMemoryStat{UsedPercent: 50}, cpuStat: []float64{10}},
		minFloor:         0,
		clusterSize:      1,
		maxThreshPercent: 90,
		minThreshPercent: 10,
		fallback:         time.Second,
		currentIndex:     0,
		start:            time.Now(),
		tracker:          tracker,
	}

	// switchTo publishes the mode and observes push success via the tracker.
	c.switchTo(0)
	if c.currentIndex != 0 {
		t.Fatalf("expected currentIndex 0, got %d", c.currentIndex)
	}
	if got := tracker.Score(common.ReplicationNone); got <= 0 {
		t.Fatalf("expected None to have a positive score after a successful switch, got %v", got)
	}
	// The broadcast duration is recorded as the mode's latency, so the scorer
	// has a real signal rather than success alone (#1146).
	if st := tracker.Stats(common.ReplicationNone); st == nil || st.LatencyNs <= 0 {
		t.Fatalf("expected a measured latency for the switched mode, got %+v", st)
	}

	// step() refreshes the order from the tracker each tick.
	c.step()
}

func TestGetMetricsNonPrimaryServer(t *testing.T) {
	// serverId != 0, should return immediately
	cfg := common.Configuration{}
	GetMetrics(context.Background(), cfg, 1, nil)
}
