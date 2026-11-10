package transport

import (
	"sync"
	"time"
)

// Defaults for denial audit suppression (#1321). Denials are client-induced, so
// auditing every one lets a caller grow the audit bucket at request rate — and
// unique keys defeat any purely key-based scheme, hence the global rate cap.
const (
	denialAuditWindow      = time.Minute
	denialAuditMaxKeys     = 1024
	denialAuditBurstPerSec = 50
	denialAuditLogEvery    = 100 // log one line per this many suppressions
)

// denialAuditLimiter decides whether a denial is worth an audit record. It is
// deliberately in-memory and bounded: the audit trail keeps an attributed sample
// while the exact volume is carried by the refusal counters.
type denialAuditLimiter struct {
	mu              sync.Mutex
	window          time.Duration
	maxKeys         int
	burst           int
	seen            map[string]time.Time
	order           []string // insertion order, for eviction from `seen`
	tokens          int
	lastRefill      time.Time
	suppressedTotal uint64
}

func newDenialAuditLimiter() *denialAuditLimiter {
	return &denialAuditLimiter{
		window:     denialAuditWindow,
		maxKeys:    denialAuditMaxKeys,
		burst:      denialAuditBurstPerSec,
		seen:       make(map[string]time.Time, denialAuditMaxKeys),
		tokens:     denialAuditBurstPerSec,
		lastRefill: time.Now(),
	}
}

// Allow reports whether this denial should be written, plus the running count of
// suppressions. Identical denials inside the window are suppressed, and so is any
// denial beyond the per-second cap.
func (l *denialAuditLimiter) Allow(key string, now time.Time) (allowed bool, suppressedTotal uint64) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if last, ok := l.seen[key]; ok && now.Sub(last) < l.window {
		l.suppressedTotal++
		return false, l.suppressedTotal
	}
	l.refill(now)
	if l.tokens <= 0 {
		l.suppressedTotal++
		return false, l.suppressedTotal
	}
	l.tokens--
	l.remember(key, now)
	return true, l.suppressedTotal
}

// len reports the size of the suppression state, for tests and bounds checks.
func (l *denialAuditLimiter) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen)
}

// refill tops the token bucket up to the burst size. Callers hold the lock.
func (l *denialAuditLimiter) refill(now time.Time) {
	elapsed := now.Sub(l.lastRefill)
	if elapsed <= 0 {
		return
	}
	added := int(elapsed / time.Second)
	if added > 0 {
		l.tokens += added
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.lastRefill = l.lastRefill.Add(time.Duration(added) * time.Second)
	}
}

// remember records a key, evicting the oldest entries to stay within capacity.
// Callers hold the lock.
func (l *denialAuditLimiter) remember(key string, now time.Time) {
	if _, ok := l.seen[key]; ok {
		l.seen[key] = now
		return
	}
	for len(l.seen) >= l.maxKeys && len(l.order) > 0 {
		oldest := l.order[0]
		l.order = l.order[1:]
		delete(l.seen, oldest)
	}
	l.seen[key] = now
	l.order = append(l.order, key)
}
