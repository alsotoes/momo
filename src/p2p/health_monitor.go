package p2p

import (
	"context"
	"log"
	"time"
)

// HealthMonitorConfig controls proactive coordination handoff.
type HealthMonitorConfig struct {
	// Interval is how often the sampler runs.
	Interval time.Duration
	// Cooldown suppresses repeated resignations while the node stays unhealthy.
	Cooldown time.Duration
	// Limits are the thresholds that trigger a resignation.
	Limits HealthLimits
}

// HealthMonitor watches local resource usage and resigns coordination roles
// when the node is unhealthy, so a struggling holder hands off instead of
// failing mid-operation (coordinator failover Phase 2).
type HealthMonitor struct {
	cfg     HealthMonitorConfig
	sampler HealthSampler
	lm      *LeaseManager
	now     func() time.Time

	lastResign time.Time
}

// NewHealthMonitor builds a monitor. A nil sampler uses the platform default;
// a nil now uses time.Now (both injectable for tests).
func NewHealthMonitor(cfg HealthMonitorConfig, sampler HealthSampler, lm *LeaseManager) *HealthMonitor {
	if sampler == nil {
		sampler = DefaultHealthSampler()
	}
	return &HealthMonitor{cfg: cfg, sampler: sampler, lm: lm, now: time.Now}
}

// Evaluate samples once and resigns when unhealthy and outside the cooldown.
// Returns the number of leases resigned.
func (m *HealthMonitor) Evaluate() int {
	snapshot := m.sampler()
	if m.cfg.Limits.Healthy(snapshot) {
		return 0
	}
	now := m.now()
	if m.cfg.Cooldown > 0 && !m.lastResign.IsZero() && now.Sub(m.lastResign) < m.cfg.Cooldown {
		return 0
	}
	_, detail := m.cfg.Limits.Exceeded(snapshot)
	m.lastResign = now
	if m.lm == nil {
		log.Printf("AUDIT: coordination handoff due to local health (%s) but no lease manager is attached", detail)
		return 0
	}
	count := m.lm.ResignAll("unhealthy node: " + detail)
	log.Printf("AUDIT: proactive coordination handoff: %s, resigned %d lease(s)", detail, count)
	return count
}

// Run evaluates on the configured interval until ctx is cancelled.
func (m *HealthMonitor) Run(ctx context.Context) {
	interval := m.cfg.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Evaluate()
		}
	}
}
