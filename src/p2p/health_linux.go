//go:build linux

package p2p

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/alsotoes/momo/src/common"
)

// cpuLinePrefix marks the aggregate line in /proc/stat. Package-level so the
// comparison allocates nothing.
var cpuLinePrefix = []byte("cpu ")

// meminfo keys are compared with the trailing colon, as /proc writes them.
var (
	memTotalKey = []byte("MemTotal:")
	memAvailKey = []byte("MemAvailable:")
)

// DefaultHealthSampler reads CPU and memory utilisation from /proc, falling back
// to goroutines only for values the kernel does not expose. The CPU value is a
// delta between consecutive samples, so the first sample reports zero rather
// than the machine's lifetime average.
func DefaultHealthSampler() HealthSampler {
	var mu sync.Mutex
	var lastBusy, lastTotal uint64

	return func() HealthSnapshot {
		snap := HealthSnapshot{Goroutines: runtime.NumGoroutine()}
		mu.Lock()
		defer mu.Unlock()
		if busy, total, err := readCPUStat(); err == nil {
			if lastTotal != 0 && total > lastTotal {
				snap.CPUPercent = float64(busy-lastBusy) / float64(total-lastTotal) * 100
			}
			lastBusy, lastTotal = busy, total
		}
		if used, err := readMemPercent(); err == nil {
			snap.MemPercent = used
		}
		return snap
	}
}

// readCPUStat reads /proc/stat and parses the aggregate line.
func readCPUStat() (busy, total uint64, err error) {
	defer common.RecoverErr("readCPUStat", &err)
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/stat: %w", err)
	}
	return parseCPUStat(data)
}

// parseCPUStat parses the aggregate "cpu " line out of /proc/stat content.
// It walks the raw bytes: no string conversion, no line slice, no field slice,
// so a sample allocates nothing (#1311).
func parseCPUStat(data []byte) (busy, total uint64, err error) {
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		if !bytes.HasPrefix(line, cpuLinePrefix) {
			continue
		}
		return parseCPUStatLine(line[len(cpuLinePrefix):])
	}
	return 0, 0, fmt.Errorf("no cpu line in /proc/stat: %w", syscall.ENOENT)
}

// parseCPUStatLine sums the jiffies of the aggregate cpu line. idle is field 3
// and iowait field 4 (both not busy); the first four fields are required.
func parseCPUStatLine(rest []byte) (busy, total uint64, err error) {
	var idle, iowait uint64
	fields := 0
	for {
		for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			break
		}
		v, n, ok := parseDecimal(rest)
		if !ok {
			return 0, 0, fmt.Errorf("non-numeric cpu field %q: %w", firstField(rest), syscall.EBADMSG)
		}
		rest = rest[n:]
		switch fields {
		case 3:
			idle = v
		case 4:
			iowait = v
		}
		total += v
		fields++
	}
	if fields < 4 {
		return 0, 0, fmt.Errorf("cpu line has %d fields, want at least 4: %w", fields, syscall.EBADMSG)
	}
	return total - idle - iowait, total, nil
}

// readMemPercent reads /proc/meminfo and parses used memory.
func readMemPercent() (pct float64, err error) {
	defer common.RecoverErr("readMemPercent", &err)
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return parseMemPercent(data)
}

// parseMemPercent computes used memory as a percentage of total from
// /proc/meminfo content, using MemAvailable when the kernel provides it. Like
// parseCPUStat it allocates nothing.
func parseMemPercent(data []byte) (float64, error) {
	var totalKB, availKB uint64
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := line[:colon+1]
		value := line[colon+1:]
		for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
			value = value[1:]
		}
		v, _, ok := parseDecimal(value)
		if !ok {
			continue
		}
		switch {
		case bytes.Equal(key, memTotalKey):
			totalKB = v
		case bytes.Equal(key, memAvailKey):
			availKB = v
		}
	}
	if totalKB == 0 {
		return 0, fmt.Errorf("MemTotal not found in /proc/meminfo: %w", syscall.EBADMSG)
	}
	return float64(totalKB-availKB) / float64(totalKB) * 100, nil
}

// parseDecimal reads consecutive decimal digits at the start of b and returns
// the value and how many bytes it consumed. Overflow is reported as failure, so
// a corrupt counter cannot silently wrap.
func parseDecimal(b []byte) (uint64, int, bool) {
	const maxUint = ^uint64(0)
	var v uint64
	i := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		d := uint64(b[i] - '0')
		if v > (maxUint-d)/10 {
			return 0, 0, false
		}
		v = v*10 + d
		i++
	}
	return v, i, i > 0
}

// firstField returns a short, allocation-light preview of a malformed token for
// the error message.
func firstField(b []byte) string {
	i := 0
	for i < len(b) && i < 16 && b[i] != ' ' && b[i] != '\t' && b[i] != '\n' {
		i++
	}
	return string(b[:i])
}
