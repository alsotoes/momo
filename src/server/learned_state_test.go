package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/metrics"
)

func learnedTestCfg(t *testing.T, coordinationEnabled bool) common.Configuration {
	t.Helper()
	dir := t.TempDir()
	return common.Configuration{
		Global: common.ConfigurationGlobal{
			ReplicationOrder: []int{1, 2, 3, 4},
		},
		Coordination: common.ConfigurationCoordination{
			Enabled:                   coordinationEnabled,
			MetricsControllerLeaseTTL: 45,
		},
		Daemons: []*common.Daemon{{Data: dir}},
	}
}

func TestBuildLearnedStateDefaults(t *testing.T) {
	metrics.SetControllerElector(nil)
	cfg := learnedTestCfg(t, false)
	st := buildLearnedState(cfg, 0)
	if st.Controller.ElectionEnabled {
		t.Fatal("election must be reported off")
	}
	if !st.Controller.IsHolder {
		t.Fatal("legacy rule: node 0 is the controller")
	}
	if st.Controller.LeaseTTLSeconds != 45 {
		t.Fatalf("ttl = %d, want 45", st.Controller.LeaseTTLSeconds)
	}
	if st.ReplicationMode.Mode != 1 || st.ReplicationMode.Index != 0 {
		t.Fatalf("unexpected default mode: %+v", st.ReplicationMode)
	}
	if st.ReplicationMode.Persisted {
		t.Fatal("nothing has been persisted yet")
	}
	if st.Adaptive.Enabled {
		t.Fatal("adaptive is off unless the loop installed a controller")
	}
}

func TestBuildLearnedStateReadsPersistedMode(t *testing.T) {
	cfg := learnedTestCfg(t, true)
	ms := common.NewCoordinationModeStore(cfg.Daemons[0].Data)
	if ms == nil {
		t.Fatal("mode store must be available with a data dir")
	}
	if err := ms.SaveMode(3); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	st := buildLearnedState(cfg, 0)
	if st.ReplicationMode.Mode != 3 || st.ReplicationMode.Index != 2 {
		t.Fatalf("mode/index = %d/%d, want 3/2", st.ReplicationMode.Mode, st.ReplicationMode.Index)
	}
	if !st.ReplicationMode.Persisted || st.ReplicationMode.Store != "coordination/replication_mode" {
		t.Fatalf("persistence = %+v", st.ReplicationMode)
	}
}

func TestBuildLearnedStateNoDaemon(t *testing.T) {
	cfg := common.Configuration{Global: common.ConfigurationGlobal{ReplicationOrder: []int{2, 4}}}
	st := buildLearnedState(cfg, 9)
	if st.ReplicationMode.Persisted {
		t.Fatal("no daemon means no store")
	}
	if st.ReplicationMode.Mode != 2 {
		t.Fatalf("mode = %d, want the first configured mode", st.ReplicationMode.Mode)
	}
}

func TestLearnedStateDoesNotLeakAbsolutePaths(t *testing.T) {
	cfg := learnedTestCfg(t, true)
	ms := common.NewCoordinationModeStore(cfg.Daemons[0].Data)
	if err := ms.SaveMode(2); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	blob, err := json.Marshal(buildLearnedState(cfg, 0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(blob)
	if strings.Contains(payload, cfg.Daemons[0].Data) || strings.Contains(payload, "/tmp/") {
		t.Fatalf("learned state leaks an absolute path: %s", payload)
	}
}

func TestLearnedStateHandlerOmittedWithoutProvider(t *testing.T) {
	mc := NewMetricsCollector()
	if mc.learnedStateNow() != nil {
		t.Fatal("no provider must render no block")
	}
}

func TestLearnedStateHandlerSurvivesPanickingProvider(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetLearnedState(func() *LearnedState { panic("boom") })
	if mc.learnedStateNow() != nil {
		t.Fatal("a panicking provider must render no block")
	}
}

func TestLearnedStateJSONShape(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetLearnedState(func() *LearnedState {
		return &LearnedState{
			Controller:      LearnedController{ElectionEnabled: true, IsHolder: true, LeaseTTLSeconds: 30},
			ReplicationMode: LearnedMode{Mode: 2, Index: 1, Order: []int{1, 2, 3, 4}, Persisted: true, Store: "coordination/replication_mode"},
			Adaptive:        LearnedAdaptive{Enabled: true, UpperPercent: 83.5, LowerPercent: 61.2, Samples: 10, Persisted: true, Store: "adaptive.db"},
		}
	})
	blob, err := json.Marshal(mc.learnedStateNow())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"election_enabled":true`, `"is_holder":true`, `"lease_ttl_seconds":30`,
		`"mode":2`, `"index":1`, `"order":[1,2,3,4]`,
		`"upper_percent":83.5`, `"samples":10`, `"store":"adaptive.db"`,
	} {
		if !strings.Contains(string(blob), want) {
			t.Fatalf("missing %s in %s", want, blob)
		}
	}
	if strings.Contains(string(blob), fmt.Sprintf("%q", "/")) {
		t.Fatalf("unexpected absolute path: %s", blob)
	}
}

func TestDetailedHealthIncludesLearnedState(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetLearnedState(func() *LearnedState {
		return &LearnedState{
			Controller:      LearnedController{ElectionEnabled: true, IsHolder: true, LeaseTTLSeconds: 30},
			ReplicationMode: LearnedMode{Mode: 2, Index: 1, Order: []int{1, 2}, Persisted: true, Store: "coordination/replication_mode"},
		}
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	mc.detailedHealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	learned, ok := body["learned_state"].(map[string]any)
	if !ok {
		t.Fatalf("learned_state missing: %s", rec.Body.String())
	}
	controller, _ := learned["controller"].(map[string]any)
	if controller["is_holder"] != true {
		t.Fatalf("controller block = %+v", controller)
	}
}
