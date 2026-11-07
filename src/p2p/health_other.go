//go:build !linux

package p2p

// DefaultHealthSampler has no /proc on this platform, so only goroutines are
// observable; CPU and memory stay zero (unknown) and their thresholds never
// fire.
func DefaultHealthSampler() HealthSampler { return GoroutineHealthSampler() }
