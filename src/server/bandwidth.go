package server

import (
	"context"
	"io"
	"sync"
	"time"
)

// chunkSize is the write granularity of a throttled push.
const chunkSize = 64 << 10

// bandwidthLimiter is a minimal token bucket: `rate` bytes per second with a
// one-second burst. It exists so rebalance pushes cannot saturate the uplink
// that foreground traffic uses (R11, #1143).
type bandwidthLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

// newBandwidthLimiter builds a limiter for the given megabytes per second.
func newBandwidthLimiter(mbps float64) *bandwidthLimiter {
	rate := mbps * 1_000_000 / 8 // bytes per second
	return &bandwidthLimiter{rate: rate, burst: rate, tokens: rate, last: time.Now()}
}

// refill adds the tokens earned since the last call. Caller holds mu.
func (l *bandwidthLimiter) refill() {
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
}

// waitN blocks until n bytes of budget are available, or the context ends.
// Callers must pass n <= burst; writeThrottled sizes chunks accordingly.
func (l *bandwidthLimiter) waitN(ctx context.Context, n int) error {
	need := float64(n)
	for {
		l.mu.Lock()
		l.refill()
		if l.tokens >= need {
			l.tokens -= need
			l.mu.Unlock()
			return nil
		}
		deficit := need - l.tokens
		wait := time.Duration(deficit / l.rate * float64(time.Second))
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// writeThrottledReader streams r into w in chunks, spending bandwidth budget
// before each write. A nil limiter degrades to a plain copy.
func writeThrottledReader(ctx context.Context, w io.Writer, r io.Reader, limiter *bandwidthLimiter) error {
	if limiter == nil {
		_, err := io.Copy(w, r)
		return err
	}
	chunk := chunkSize
	if int(limiter.burst) < chunk {
		chunk = int(limiter.burst)
	}
	if chunk < 1 {
		chunk = 1
	}
	buf := make([]byte, chunk)
	for {
		n, rerr := r.Read(buf)
		if err := copyChunk(ctx, w, buf[:n], limiter); err != nil {
			return err
		}
		if rerr != nil {
			return eofOr(rerr)
		}
	}
}

// copyChunk spends budget and writes one chunk.
func copyChunk(ctx context.Context, w io.Writer, chunk []byte, limiter *bandwidthLimiter) error {
	if len(chunk) == 0 {
		return nil
	}
	if err := limiter.waitN(ctx, len(chunk)); err != nil {
		return err
	}
	_, err := w.Write(chunk)
	return err
}

// eofOr maps io.EOF to nil so copy loops end cleanly.
func eofOr(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

// writeThrottled copies data to w in chunks, spending bandwidth budget before
// each chunk. A nil limiter writes everything in one call.
func writeThrottled(ctx context.Context, w io.Writer, data []byte, limiter *bandwidthLimiter) error {
	if limiter == nil {
		_, err := w.Write(data)
		return err
	}
	chunk := chunkSize
	if int(limiter.burst) < chunk {
		chunk = int(limiter.burst)
	}
	if chunk < 1 {
		chunk = 1
	}
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		part := data[off:end]
		if err := limiter.waitN(ctx, len(part)); err != nil {
			return err
		}
		if _, err := w.Write(part); err != nil {
			return err
		}
	}
	return nil
}
