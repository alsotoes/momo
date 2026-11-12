package replication_order

import "sync"

// ModeScore is one mode's ranking state, exported as Prometheus gauges (#1146).
type ModeScore struct {
	Mode    int
	Score   float64
	Samples int
}

// exportMu guards exportSnapshot: the scrape path and the controller tick run
// on different goroutines.
var (
	exportMu       sync.RWMutex
	exportSnapshot *Reorderer
)

// SetExportSnapshot publishes the active reorderer for metrics export, or nil
// when ordering is disabled. The exporter then reports nothing rather than a
// misleading zero.
func SetExportSnapshot(r *Reorderer) {
	exportMu.Lock()
	exportSnapshot = r
	exportMu.Unlock()
}

// Scores returns one entry per tracked mode, in the configured mode order so a
// scrape is stable. ok is false when no tracker is active.
func Scores() (scores []ModeScore, ok bool) {
	exportMu.RLock()
	r := exportSnapshot
	exportMu.RUnlock()
	if r == nil {
		return nil, false
	}
	modes := r.Modes()
	scores = make([]ModeScore, 0, len(modes))
	for _, mode := range modes {
		entry := ModeScore{Mode: mode}
		if st := r.Stats(mode); st != nil {
			entry.Score = st.Score
			entry.Samples = st.Samples
		}
		scores = append(scores, entry)
	}
	return scores, true
}
