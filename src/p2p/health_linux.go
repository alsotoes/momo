//go:build linux

package p2p

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

// readCPUStat returns busy and total jiffies from the aggregate cpu line.
func readCPUStat() (busy, total uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var values []uint64
		for _, f := range fields {
			v, perr := strconv.ParseUint(f, 10, 64)
			if perr != nil {
				return 0, 0, perr
			}
			values = append(values, v)
		}
		if len(values) < 4 {
			return 0, 0, os.ErrInvalid
		}
		for _, v := range values {
			total += v
		}
		// user, nice, system, irq, softirq, steal are busy; idle + iowait are not.
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		busy = total - idle
		return busy, total, nil
	}
	return 0, 0, os.ErrNotExist
}

// readMemPercent returns used memory as a percentage of total, using
// MemAvailable when the kernel provides it.
func readMemPercent() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var totalKB, availKB float64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, perr := strconv.ParseFloat(fields[1], 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	if totalKB == 0 {
		return 0, os.ErrInvalid
	}
	return (totalKB - availKB) / totalKB * 100, nil
}
