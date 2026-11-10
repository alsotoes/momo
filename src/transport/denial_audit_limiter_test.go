package transport

import (
	"sync"
	"testing"
	"time"
)

// TestDenialLimiterSuppressesRepeats pins the per-key window: an identical denial
// is written once, and the counter keeps counting (#1321).
func TestDenialLimiterSuppressesRepeats(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()

	if allowed, _ := l.Allow("acme|Authorize|object/b/k", now); !allowed {
		t.Fatal("the first denial must be audited")
	}
	for i := 0; i < 5; i++ {
		if allowed, suppressed := l.Allow("acme|Authorize|object/b/k", now.Add(time.Duration(i)*time.Second)); allowed {
			t.Fatalf("repeat %d inside the window must be suppressed", i)
		} else if suppressed == 0 {
			t.Fatal("suppression must be counted so the caller can log it")
		}
	}
	if got := l.len(); got != 1 {
		t.Fatalf("suppression state must hold one key, got %d", got)
	}
}

// TestDenialLimiterReopensAfterWindow proves suppression is a rate limit, not a
// permanent mute: the same denial is audited again in the next window.
func TestDenialLimiterReopensAfterWindow(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()

	if allowed, _ := l.Allow("acme|Authorize|object/b/k", now); !allowed {
		t.Fatal("first denial must be audited")
	}
	if allowed, _ := l.Allow("acme|Authorize|object/b/k", now.Add(denialAuditWindow+time.Second)); !allowed {
		t.Fatal("a denial after the window must be audited again")
	}
}

// TestDenialLimiterCapsDistinctKeys bounds the unique-key flood: dedupe alone
// cannot help when every request names a different resource.
func TestDenialLimiterCapsDistinctKeys(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()

	admitted := 0
	for i := 0; i < denialAuditBurstPerSec*4; i++ {
		key := "acme|Authorize|object/b/key-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if allowed, _ := l.Allow(key, now); allowed {
			admitted++
		}
	}
	if admitted != denialAuditBurstPerSec {
		t.Fatalf("expected the burst cap of %d distinct entries, got %d", denialAuditBurstPerSec, admitted)
	}
}

// TestDenialLimiterRefillsOverTime proves the bucket recovers, so a long-running
// daemon keeps producing samples.
func TestDenialLimiterRefillsOverTime(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()
	for i := 0; i < denialAuditBurstPerSec; i++ {
		l.Allow("acme|Authorize|object/b/"+time.Duration(i).String(), now)
	}
	if allowed, _ := l.Allow("acme|Authorize|late", now); allowed {
		t.Fatal("bucket must be exhausted")
	}
	if allowed, _ := l.Allow("acme|Authorize|late", now.Add(2*time.Second)); !allowed {
		t.Fatal("bucket must refill after a second")
	}
}

// TestDenialLimiterStateIsBounded keeps the suppression state from becoming the
// memory leak it exists to prevent.
func TestDenialLimiterStateIsBounded(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()
	for i := 0; i < denialAuditMaxKeys*3; i++ {
		l.Allow("acme|Authorize|"+time.Duration(i).String(), now.Add(time.Duration(i)*time.Second))
	}
	if got := l.len(); got > denialAuditMaxKeys {
		t.Fatalf("suppression state grew to %d, cap is %d", got, denialAuditMaxKeys)
	}
}

// TestDenialLimiterConcurrent hammers the limiter exactly as concurrent
// connections would, under -race.
func TestDenialLimiterConcurrent(t *testing.T) {
	l := newDenialAuditLimiter()
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				l.Allow("acme|Authorize|object/b/shared", now)
			}
		}(i)
	}
	wg.Wait()
	if got := l.len(); got > 1 {
		t.Fatalf("shared key must collapse to one entry, got %d", got)
	}
}

// TestNilDenialLimiterAllows keeps the nil receiver safe, so a communicator built
// in a test without the limiter still audits.
func TestNilDenialLimiterAllows(t *testing.T) {
	var l *denialAuditLimiter
	if allowed, _ := l.Allow("k", time.Now()); !allowed {
		t.Fatal("a nil limiter must not suppress")
	}
}
