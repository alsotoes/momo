package server

import (
	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/metrics"
)

// LearnedState is the read-only view of what this node learned at runtime and
// whether that state is persisted (#1287). It is rendered by /health/detailed.
//
// Store names are relative to the node's data directory on purpose: the health
// endpoint is unauthenticated, so an absolute path would leak the layout.
type LearnedState struct {
	Controller      LearnedController `json:"controller"`
	ReplicationMode LearnedMode       `json:"replication_mode"`
	Adaptive        LearnedAdaptive   `json:"adaptive"`
}

// LearnedController reports the coordination-role election status.
type LearnedController struct {
	ElectionEnabled bool `json:"election_enabled"`
	IsHolder        bool `json:"is_holder"`
	LeaseTTLSeconds int  `json:"lease_ttl_seconds"`
}

// LearnedMode reports the polymorphic replication mode the node resumed.
type LearnedMode struct {
	Mode      int    `json:"mode"`
	Index     int    `json:"index"`
	Order     []int  `json:"order"`
	Persisted bool   `json:"persisted"`
	Store     string `json:"store,omitempty"`
}

// LearnedAdaptive reports the self-tuned CPU/memory thresholds.
type LearnedAdaptive struct {
	Enabled      bool    `json:"enabled"`
	UpperPercent float64 `json:"upper_percent"`
	LowerPercent float64 `json:"lower_percent"`
	Samples      int     `json:"samples"`
	Persisted    bool    `json:"persisted"`
	Store        string  `json:"store,omitempty"`
}

// buildLearnedState assembles the block from configuration and the persisted
// mode store. It performs one small file read per call (the detailed-health
// request), never on the metrics scrape path.
func buildLearnedState(cfg common.Configuration, serverId int) *LearnedState {
	order := cfg.Global.ReplicationOrder
	st := &LearnedState{
		Controller: LearnedController{
			ElectionEnabled: cfg.Coordination.Enabled,
			IsHolder:        metrics.IsControllerNode(serverId),
			LeaseTTLSeconds: cfg.Coordination.MetricsControllerLeaseTTL,
		},
		ReplicationMode: LearnedMode{
			Index: 0,
			Order: order,
		},
	}
	if len(order) > 0 {
		st.ReplicationMode.Mode = order[0]
	}

	if mode, store, ok := persistedMode(cfg, serverId); ok {
		st.ReplicationMode.Mode = mode
		st.ReplicationMode.Index = modeIndex(order, mode)
		st.ReplicationMode.Persisted = store != ""
		st.ReplicationMode.Store = store
	}

	if upper, lower, samples, ok := metrics.AdaptiveGauges(); ok {
		st.Adaptive.Enabled = true
		st.Adaptive.UpperPercent = upper
		st.Adaptive.LowerPercent = lower
		st.Adaptive.Samples = samples
	}
	if store, ok := metrics.AdaptivePersistence(); ok {
		st.Adaptive.Persisted = true
		st.Adaptive.Store = store
	}
	return st
}

// persistedMode reads the mode store for this node. It returns ok=false when the
// node has no data directory or nothing was persisted yet.
func persistedMode(cfg common.Configuration, serverId int) (mode int, store string, ok bool) {
	if serverId < 0 || serverId >= len(cfg.Daemons) || cfg.Daemons[serverId] == nil {
		return 0, "", false
	}
	ms := common.NewCoordinationModeStore(cfg.Daemons[serverId].Data)
	if ms == nil {
		return 0, "", false
	}
	mode, found, err := ms.LoadMode()
	if err != nil || !found {
		return 0, "", false
	}
	return mode, "coordination/replication_mode", true
}

// modeIndex returns the position of mode in order, or 0 when unknown.
func modeIndex(order []int, mode int) int {
	for i, m := range order {
		if m == mode {
			return i
		}
	}
	return 0
}
