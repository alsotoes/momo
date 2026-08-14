package common

import (
	"log"
	"sync"
	"time"
)

// authLimiterDefaultFactor is the multiplier applied to the base delay for each
// consecutive failure, producing an exponential backoff curve.
const authLimiterDefaultFactor = 2

// authLimiterDefaultMaxDelay bounds how large the exponential backoff can grow,
// preventing an unbounded sleep for a source that never authenticates.
const authLimiterDefaultMaxDelay = 8 * time.Second

// authLimiterDefaultMaxFailures is the consecutive-failure threshold beyond
// which a source enters a temporary lockout.
const authLimiterDefaultMaxFailures = 5

// authLimiterDefaultIdleWindow is how long a source entry may sit idle before
// it is evicted, bounding the map's memory footprint (Rule 4 / Rule 32).
const authLimiterDefaultIdleWindow = 5 * time.Minute

// authLimiterEntry is the per-source state tracked by AuthLimiter.
type authLimiterEntry struct {
	failures     int
	nextAllow    time.Time
	lockoutUntil time.Time
	lastSeen     time.Time
}

// AuthLimiter throttles repeated failed authentication attempts per source,
// applying adaptive exponential backoff and a temporary lockout. Is is safe for
// concurrent use from many connection-handler goroutines.
//
// It is disabled (Allow always true, Record* no-op) when baseDelay is zero, so
// existing deployments observe no behavioral change unless explicitly enabled.
type AuthLimiter struct {
	mu sync.Mutex

	baseDelay   time.Duration
	factor      int
	maxDelay    time.Duration
	maxFailures int
	lockout     time.Duration
	idleWindow  time.Duration
	now         func() time.Time
	sources     map[string]*authLimiterEntry
}

// NewAuthLimiter constructs an AuthLimiter with the given base delay (the delay
// applied after the first failure). All other tuning values take defaults that
// yield an exponential curve bounded by maxDelay and a lockout past
// maxFailures. A baseDelay <= 0 creates a disabled limiter.
func NewAuthLimiter(baseDelay time.Duration) *AuthLimiter {
	l := &AuthLimiter{
		baseDelay:   baseDelay,
		factor:      authLimiterDefaultFactor,
		maxDelay:    authLimiterDefaultMaxDelay,
		maxFailures: authLimiterDefaultMaxFailures,
		idleWindow:  authLimiterDefaultIdleWindow,
		now:         time.Now,
		sources:     make(map[string]*authLimiterEntry),
	}
	if baseDelay <= 0 {
		l.lockout = 0
	} else {
		// Scale the lockout window from the base delay so short bases give a
		// short lockout and long bases give a proportionally longer one, but
		// never less than the max backoff delay.
		l.lockout = baseDelay * 60
		if l.lockout > 60*time.Second {
			l.lockout = 60 * time.Second
		}
		if l.lockout < l.maxDelay {
			l.lockout = l.maxDelay
		}
	}
	return l
}

// Enabled reports whether the limiter actively throttles.
func (l *AuthLimiter) Enabled() bool {
	if l == nil {
		return false
	}
	return l.baseDelay > 0
}

// Allow reports whether the given source may currently attempt authentication.
// A false return means the source is under backoff or lockout; the caller must
// reject the attempt. It never blocks.
func (l *AuthLimiter) Allow(source string) bool {
	if !l.Enabled() {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	e := l.sources[source]
	if e == nil {
		// No entry for this source: opportunistically sweep idle entries so a
		// large, slowly-churning source space cannot grow the map unboundedly.
		l.evictIdle(now)
		return true
	}
	if now.Before(e.lockoutUntil) {
		return false
	}
	if now.Before(e.nextAllow) {
		return false
	}
	return true
}

// RecordFailure registers a failed authentication from source and returns the
// delay the next attempt from that source must wait (0 if unlimited). After
// maxFailures consecutive failures the source enters a temporary lockout.
func (l *AuthLimiter) RecordFailure(source string) time.Duration {
	if !l.Enabled() {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	e := l.sources[source]
	if e == nil {
		e = &authLimiterEntry{}
		l.sources[source] = e
	}
	e.failures++
	e.lastSeen = now

	if e.failures >= l.maxFailures {
		// Enter/refresh lockout; log only on the transition into an active
		// lockout to avoid spamming the log for every subsequent failure.
		if !now.Before(e.lockoutUntil) || e.failures == l.maxFailures {
			log.Printf("AUTH: source %s locked out for %s after %d consecutive failures", SanitizeLog(source), l.lockout, e.failures)
		}
		e.lockoutUntil = now.Add(l.lockout)
		e.nextAllow = e.lockoutUntil
		return l.lockout
	}

	// Exponential backoff: delay = min(base * factor^(failures-1), maxDelay).
	delay := l.backoffDelay(e.failures)
	e.nextAllow = now.Add(delay)
	return delay
}

// RecordSuccess clears the state for a source that has authenticated
// successfully, releasing any backoff or lockout immediately.
func (l *AuthLimiter) RecordSuccess(source string) {
	if !l.Enabled() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sources, source)
}

// backoffDelay computes the adaptive delay for the given failure count.
func (l *AuthLimiter) backoffDelay(failures int) time.Duration {
	delay := l.baseDelay
	for i := 1; i < failures; i++ {
		delay *= time.Duration(l.factor)
		if delay >= l.maxDelay {
			return l.maxDelay
		}
	}
	if delay > l.maxDelay {
		return l.maxDelay
	}
	return delay
}

// evictIdle removes entries whose lastSeen is older than the idle window.
// It is called opportunistically on Allow/Record from a bounded source count to
// keep the map's memory bounded (Rule 4 / Rule 32).
func (l *AuthLimiter) evictIdle(now time.Time) {
	window := l.idleWindow
	for src, e := range l.sources {
		if now.Sub(e.lastSeen) > window {
			delete(l.sources, src)
		}
	}
}
