package common

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestAuthLimiterDisabled(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(0)
	if l.Enabled() {
		t.Fatal("expected disabled limiter for baseDelay=0")
	}
	if !l.Allow("1.2.3.4:5678") {
		t.Fatal("disabled limiter must allow all")
	}
	if got := l.RecordFailure("1.2.3.4:5678"); got != 0 {
		t.Fatalf("disabled limiter must return 0 delay, got %v", got)
	}
	// No-op success should be safe.
	l.RecordSuccess("1.2.3.4:5678")
}

func TestAuthLimiterBackoffMonotonicAndCapped(t *testing.T) {
	defer goleak.VerifyNone(t)

	base := 10 * time.Millisecond
	l := NewAuthLimiter(base)

	fake := time.Now()
	l.now = func() time.Time { return fake }

	src := "10.0.0.1:4000"
	var prev time.Duration
	seen := false
	// Drive past maxFailures; delays before lockout should grow monotonically.
	for i := 1; i < l.maxFailures; i++ {
		got := l.RecordFailure(src)
		if i < l.maxFailures {
			// Advance past the backoff delay so the next failure is allowed.
			fake = fake.Add(got + time.Nanosecond)
		}
		if got > l.maxDelay {
			t.Fatalf("attempt %d delay %v exceeds maxDelay %v", i, got, l.maxDelay)
		}
		if seen && got <= prev {
			t.Fatalf("backoff not monotonic: after %d failures got %v <= prev %v", i, got, prev)
		}
		prev = got
		seen = true
	}
}

func TestAuthLimiterBackoffGrowthValue(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(10 * time.Millisecond)
	src := "10.0.0.2:4000"

	d1 := l.RecordFailure(src)
	d2 := l.RecordFailure(src)
	if d2 <= d1 {
		t.Fatalf("expected d2>d1, got d1=%v d2=%v", d1, d2)
	}
	// With factor 2: d2 should be 2*d1 (within granularity).
	expected := d1 * 2
	if d2 != expected {
		t.Fatalf("expected 2x growth d2=%v, want %v", d2, expected)
	}
}

func TestAuthLimiterLockoutThreshold(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(10 * time.Millisecond)
	src := "10.0.0.3:4000"

	for i := 0; i < l.maxFailures; i++ {
		l.RecordFailure(src)
	}
	if l.Allow(src) {
		t.Fatalf("source must be locked out after %d consecutive failures", l.maxFailures)
	}
}

func TestAuthLimiterLockoutExpiry(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(10 * time.Millisecond)
	src := "10.0.0.4:4000"

	// Pin the clock so we can advance time deterministically.
	fake := time.Now()
	l.now = func() time.Time { return fake }

	for i := 0; i < l.maxFailures; i++ {
		l.RecordFailure(src)
	}
	if l.Allow(src) {
		t.Fatal("expected lockout active")
	}

	// Advance past the lockout window.
	fake = fake.Add(l.lockout + time.Nanosecond)
	if !l.Allow(src) {
		t.Fatal("expected lockout to expire after lockout duration")
	}
}

func TestAuthLimiterResetOnSuccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(10 * time.Millisecond)
	src := "10.0.0.5:4000"

	for i := 0; i < l.maxFailures; i++ {
		l.RecordFailure(src)
	}
	if l.Allow(src) {
		t.Fatal("expected lockout before success")
	}

	l.RecordSuccess(src)
	if !l.Allow(src) {
		t.Fatal("expected success to clear lockout")
	}
}

func TestAuthLimiterIdleEviction(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(10 * time.Millisecond)
	fake := time.Now()
	l.now = func() time.Time { return fake }
	l.idleWindow = time.Minute

	src := "10.0.0.6:4000"
	l.RecordFailure(src)
	// Entry is present, but the backoff delay is active until it elapses.
	fake = fake.Add(l.backoffDelay(1) + time.Nanosecond)
	if !l.Allow(src) {
		t.Fatal("expected allowed once backoff delay elapses")
	}

	// Idle long enough to be evicted.
	l.mu.Lock()
	_, has := l.sources[src]
	l.mu.Unlock()
	if !has {
		t.Fatal("expected entry present before idle window elapses")
	}

	// Eviction is opportunistic on Allow of a fresh source.
	fake = fake.Add(2 * time.Minute)
	other := "10.0.0.7:4000"
	if !l.Allow(other) {
		t.Fatal("fresh source must be allowed")
	}
	// The idle entry should now have been swept.
	l.mu.Lock()
	_, has = l.sources[src]
	l.mu.Unlock()
	if has {
		t.Fatal("expected idle source to be evicted")
	}
}

func TestAuthLimiterConcurrent(t *testing.T) {
	defer goleak.VerifyNone(t)

	l := NewAuthLimiter(time.Millisecond)
	srcs := []string{"10.0.0.10:4000", "10.0.0.11:4000", "10.0.0.12:4000"}
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 500; j++ {
				for _, s := range srcs {
					l.RecordFailure(s)
					_ = l.Allow(s)
				}
				l.RecordSuccess(srcs[0])
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
