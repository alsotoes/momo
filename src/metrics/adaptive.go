package metrics

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	bolt "go.etcd.io/bbolt"
)

// adaptiveBucket stores the learned threshold state.
var adaptiveBucket = []byte("adaptive_thresholds")

// EWMALearner tracks a smoothed mean and variance of a stream of samples.
// It is safe for concurrent use.
type EWMALearner struct {
	mu       sync.Mutex
	alpha    float64
	samples  int
	mean     float64
	variance float64
}

// NewEWMALearner builds a learner with the given smoothing factor in (0,1].
func NewEWMALearner(alpha float64) *EWMALearner {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.2
	}
	return &EWMALearner{alpha: alpha}
}

// Add records one sample.
func (l *EWMALearner) Add(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.samples == 0 {
		l.mean = v
		l.variance = 0
		l.samples = 1
		return
	}
	delta := v - l.mean
	l.mean += l.alpha * delta
	l.variance = (1 - l.alpha) * (l.variance + l.alpha*delta*delta)
	if l.samples < math.MaxInt32 {
		l.samples++
	}
}

// Mean returns the smoothed mean and the number of samples seen.
func (l *EWMALearner) Mean() (float64, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mean, l.samples
}

// StdDev returns the smoothed standard deviation.
func (l *EWMALearner) StdDev() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return math.Sqrt(l.variance)
}

// ThresholdBounds clamp a learned threshold to a safe operating range.
const (
	adaptiveMaxCeiling = 95.0
	adaptiveMaxFloor   = 60.0
	adaptiveMinCeiling = 60.0
	adaptiveMinFloor   = 20.0
	// adaptiveStepPercent is the minimum change worth applying: smaller moves
	// would make the control loop twitch on noise.
	adaptiveStepPercent = 5.0
)

// LearnedThresholds is the pair of thresholds a learner currently recommends.
type LearnedThresholds struct {
	Max float64
	Min float64
}

// ComputeThresholds derives safe thresholds from observed utilisation.
//
// The maximum rises with the observed baseline (mean + 2σ, plus a margin) and
// never drops below the configured value, so a quiet cluster keeps its operator
// setting; the minimum tracks the observed quiet level, clamped below the
// maximum. Both are clamped to [adaptiveMinFloor, adaptiveMaxCeiling] and the
// minimum always keeps a gap of at least 10 points under the maximum.
func ComputeThresholds(cfgMax, cfgMin float64, mean, stddev float64) LearnedThresholds {
	max := math.Max(cfgMax, mean+2*stddev+5)
	max = math.Min(math.Max(max, adaptiveMaxFloor), adaptiveMaxCeiling)
	min := math.Min(cfgMin, mean*0.8)
	min = math.Min(math.Max(min, adaptiveMinFloor), adaptiveMinCeiling)
	if max-min < 10 {
		min = max - 10
	}
	if min < adaptiveMinFloor {
		min = adaptiveMinFloor
	}
	return LearnedThresholds{Max: math.Round(max), Min: math.Round(min)}
}

// AdaptiveThresholds couples the learner with persistence and hysteresis.
type AdaptiveThresholds struct {
	learner    *EWMALearner
	minSamples int
	cfgMax     float64
	cfgMin     float64

	mu        sync.Mutex
	applied   LearnedThresholds
	lastSave  time.Time
	saveEvery time.Duration
	db        *bolt.DB
}

// NewAdaptiveThresholds builds an adaptive controller. dataDir enables
// persistence (an empty directory keeps it in memory only).
func NewAdaptiveThresholds(alpha float64, minSamples int, cfgMax, cfgMin float64, persistEvery time.Duration, dataDir string) (*AdaptiveThresholds, error) {
	a := &AdaptiveThresholds{
		learner:    NewEWMALearner(alpha),
		minSamples: minSamples,
		cfgMax:     cfgMax,
		cfgMin:     cfgMin,
		applied:    LearnedThresholds{Max: cfgMax, Min: cfgMin},
		saveEvery:  persistEvery,
	}
	if dataDir == "" {
		return a, nil
	}
	db, err := bolt.Open(filepath.Join(dataDir, "adaptive.db"), 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open adaptive store: %w", err)
	}
	a.db = db
	if err := a.load(); err != nil {
		log.Printf("AUDIT: could not load learned thresholds: %v — starting from configuration", common.SanitizeLog(err.Error()))
	}
	return a, nil
}

// Close releases the persistence handle.
func (a *AdaptiveThresholds) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

// load restores the last persisted thresholds.
func (a *AdaptiveThresholds) load() (err error) {
	defer common.RecoverErr("AdaptiveThresholds.load", &err)
	return a.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(adaptiveBucket)
		if b == nil {
			return nil
		}
		maxRaw := b.Get([]byte("max"))
		minRaw := b.Get([]byte("min"))
		if maxRaw == nil || minRaw == nil {
			return nil
		}
		maxV, err := strconv.ParseFloat(string(maxRaw), 64)
		if err != nil {
			return fmt.Errorf("parse learned max: %w", syscall.EINVAL)
		}
		minV, err := strconv.ParseFloat(string(minRaw), 64)
		if err != nil {
			return fmt.Errorf("parse learned min: %w", syscall.EINVAL)
		}
		// Never adopt a persisted value that would violate the clamps.
		candidate := ComputeThresholds(a.cfgMax, a.cfgMin, maxV-5, (maxV-a.cfgMax)/2)
		a.mu.Lock()
		a.applied = LearnedThresholds{Max: math.Max(candidate.Max, maxV), Min: math.Min(candidate.Min, minV)}
		a.mu.Unlock()
		log.Printf("ADAPTIVE: restored thresholds max=%.0f%% min=%.0f%%", a.applied.Max, a.applied.Min)
		return nil
	})
}

// Observe feeds one utilisation sample and returns the thresholds to use.
func (a *AdaptiveThresholds) Observe(usedPercent float64) LearnedThresholds {
	a.learner.Add(usedPercent)
	mean, samples := a.learner.Mean()
	if samples < a.minSamples {
		return a.current()
	}
	next := ComputeThresholds(a.cfgMax, a.cfgMin, mean, a.learner.StdDev())
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := math.Abs(next.Max-a.applied.Max) >= adaptiveStepPercent ||
		math.Abs(next.Min-a.applied.Min) >= adaptiveStepPercent
	if !changed {
		return a.applied
	}
	log.Printf("ADAPTIVE: thresholds adjusted max %.0f%% -> %.0f%%, min %.0f%% -> %.0f%% (samples=%d)",
		a.applied.Max, next.Max, a.applied.Min, next.Min, samples)
	a.applied = next
	return a.applied
}

// current returns the thresholds in force.
func (a *AdaptiveThresholds) current() LearnedThresholds {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applied
}

// Thresholds returns the thresholds currently in force.
func (a *AdaptiveThresholds) Thresholds() LearnedThresholds {
	if a == nil {
		return LearnedThresholds{}
	}
	return a.current()
}

// MaybePersist writes the learned thresholds when the interval elapsed.
func (a *AdaptiveThresholds) MaybePersist(now time.Time) {
	if a == nil || a.db == nil || a.saveEvery <= 0 {
		return
	}
	a.mu.Lock()
	if now.Sub(a.lastSave) < a.saveEvery {
		a.mu.Unlock()
		return
	}
	a.lastSave = now
	snap := a.applied
	a.mu.Unlock()

	err := a.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(adaptiveBucket)
		if err != nil {
			return err
		}
		if err := b.Put([]byte("max"), []byte(strconv.FormatFloat(snap.Max, 'f', -1, 64))); err != nil {
			return err
		}
		return b.Put([]byte("min"), []byte(strconv.FormatFloat(snap.Min, 'f', -1, 64)))
	})
	if err != nil {
		log.Printf("AUDIT: failed to persist learned thresholds: %v", common.SanitizeLog(err.Error()))
	}
}

// adaptiveSnapshot is the process-wide view exported as gauges, so the scrape
// path does not need a handle on the controller.
var (
	adaptiveMu       sync.RWMutex
	adaptiveSnapshot *AdaptiveThresholds
)

// setAdaptiveSnapshot publishes the active controller for metrics export.
func setAdaptiveSnapshot(a *AdaptiveThresholds) {
	adaptiveMu.Lock()
	adaptiveSnapshot = a
	adaptiveMu.Unlock()
}

// AdaptiveGauges returns the learned thresholds and sample count for export.
func AdaptiveGauges() (max, min float64, samples int, ok bool) {
	adaptiveMu.RLock()
	a := adaptiveSnapshot
	adaptiveMu.RUnlock()
	if a == nil {
		return 0, 0, 0, false
	}
	t := a.Thresholds()
	_, n := a.learner.Mean()
	return t.Max, t.Min, n, true
}

// dataDirFor returns a writable directory for adaptive state, or "" when the
// node has no configured data directory.
func dataDirFor(daemons []*common.Daemon, serverID int) string {
	if serverID < 0 || serverID >= len(daemons) || daemons[serverID] == nil {
		return ""
	}
	dir := daemons[serverID].Data
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
