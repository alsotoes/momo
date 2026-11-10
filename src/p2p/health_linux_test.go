//go:build linux

package p2p

import (
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantBusy  uint64
		wantTotal uint64
		wantErr   error
	}{
		{
			name: "aggregate line with iowait",
			data: "cpu  100 20 30 40 10 5 5 0 0 0\ncpu0 50 10 15 20 5 2 2 0 0 0\n",
			// total = 100+20+30+40+10+5+5 = 210; idle = 40+10 = 50
			wantBusy:  160,
			wantTotal: 210,
		},
		{
			name:      "four fields only, no iowait",
			data:      "cpu  1 2 3 4\n",
			wantBusy:  6,
			wantTotal: 10,
		},
		{
			name:      "no trailing newline",
			data:      "cpu  1 2 3 4",
			wantBusy:  6,
			wantTotal: 10,
		},
		{
			name:    "missing aggregate line",
			data:    "cpu0 1 2 3 4\nintr 10\n",
			wantErr: syscall.ENOENT,
		},
		{
			name:    "too few fields",
			data:    "cpu  1 2 3\n",
			wantErr: syscall.EBADMSG,
		},
		{
			name:    "non-numeric field",
			data:    "cpu  1 2 three 4\n",
			wantErr: syscall.EBADMSG,
		},
		{
			name:    "empty input",
			data:    "",
			wantErr: syscall.ENOENT,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			busy, total, err := parseCPUStat([]byte(tc.data))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if busy != tc.wantBusy || total != tc.wantTotal {
				t.Fatalf("busy,total = %d,%d, want %d,%d", busy, total, tc.wantBusy, tc.wantTotal)
			}
		})
	}
}

func TestParseCPUStatOverflowRejected(t *testing.T) {
	// 21 nines exceeds uint64; must fail instead of wrapping.
	if _, _, err := parseCPUStat([]byte("cpu  999999999999999999999 2 3 4\n")); !errors.Is(err, syscall.EBADMSG) {
		t.Fatalf("err = %v, want EBADMSG", err)
	}
}

func TestParseMemPercent(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    float64
		wantErr error
	}{
		{
			name: "half used",
			data: "MemTotal:       1000 kB\nMemFree:         500 kB\nMemAvailable:    500 kB\n",
			want: 50,
		},
		{
			name: "available missing counts as fully used",
			data: "MemTotal:       1000 kB\nMemFree:         500 kB\n",
			want: 100,
		},
		{
			name: "irrelevant lines ignored",
			data: "HugePages_Total: 0\nMemTotal: 2000 kB\nMemAvailable: 500 kB\n",
			want: 75,
		},
		{
			name: "no trailing newline",
			data: "MemTotal: 1000 kB\nMemAvailable: 250 kB",
			want: 75,
		},
		{
			name:    "missing MemTotal",
			data:    "MemFree: 10 kB\n",
			wantErr: syscall.EBADMSG,
		},
		{
			name:    "empty input",
			data:    "",
			wantErr: syscall.EBADMSG,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pct, err := parseMemPercent([]byte(tc.data))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pct != tc.want {
				t.Fatalf("pct = %v, want %v", pct, tc.want)
			}
		})
	}
}

func TestParseDecimal(t *testing.T) {
	tests := []struct {
		in     string
		want   uint64
		wantN  int
		wantOK bool
	}{
		{"123 kB", 123, 3, true},
		{"0", 0, 1, true},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
		{"99999999999999999999999", 0, 0, false},
	}
	for _, tc := range tests {
		v, n, ok := parseDecimal([]byte(tc.in))
		if v != tc.want || n != tc.wantN || ok != tc.wantOK {
			t.Fatalf("parseDecimal(%q) = %d,%d,%v want %d,%d,%v", tc.in, v, n, ok, tc.want, tc.wantN, tc.wantOK)
		}
	}
}

// The two tests below read the real /proc and are therefore Linux-only smoke
// tests: they prove the file readers and the parser agree on real kernel output.

func TestReadCPUStatFromProc(t *testing.T) {
	busy, total, err := readCPUStat()
	if err != nil {
		t.Fatalf("readCPUStat: %v", err)
	}
	if total == 0 {
		t.Fatal("total jiffies must be non-zero")
	}
	if busy > total {
		t.Fatalf("busy %d exceeds total %d", busy, total)
	}
}

func TestReadMemPercentFromProc(t *testing.T) {
	pct, err := readMemPercent()
	if err != nil {
		t.Fatalf("readMemPercent: %v", err)
	}
	if pct < 0 || pct > 100 {
		t.Fatalf("memory percent out of range: %f", pct)
	}
}

func TestDefaultHealthSamplerDelta(t *testing.T) {
	sampler := DefaultHealthSampler()
	first := sampler()
	if first.Goroutines <= 0 {
		t.Fatalf("goroutines must be reported, got %d", first.Goroutines)
	}
	if first.CPUPercent != 0 {
		t.Fatalf("the first sample has no delta and must report 0, got %f", first.CPUPercent)
	}
	second := sampler()
	if second.CPUPercent < 0 || second.CPUPercent > 100 {
		t.Fatalf("cpu percent out of range: %f", second.CPUPercent)
	}
	if second.MemPercent < 0 || second.MemPercent > 100 {
		t.Fatalf("memory percent out of range: %f", second.MemPercent)
	}
}

var (
	benchCPUStatData = []byte(strings.Repeat("cpu0 1 2 3 4 5 6 7 0 0 0\n", 32) + "cpu  100 20 30 40 10 5 5 0 0 0\n")
	benchMemInfoData = []byte(strings.Repeat("SomeLine: 12345 kB\n", 20) + "MemTotal: 16384000 kB\nMemAvailable: 4096000 kB\n")
)

func BenchmarkParseCPUStat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := parseCPUStat(benchCPUStatData); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseMemPercent(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := parseMemPercent(benchMemInfoData); err != nil {
			b.Fatal(err)
		}
	}
}

// splitBaseline* reproduce the pre-#1311 algorithm so the benchmark records the
// measured cost of the change rather than only its absolute result.
func splitBaselineCPUStat(data []byte) (busy, total uint64, err error) {
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
			return 0, 0, syscall.EBADMSG
		}
		for _, v := range values {
			total += v
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return total - idle, total, nil
	}
	return 0, 0, syscall.ENOENT
}

func splitBaselineMemPercent(data []byte) (float64, error) {
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
		return 0, syscall.EBADMSG
	}
	return (totalKB - availKB) / totalKB * 100, nil
}

func BenchmarkParseCPUStatSplitBaseline(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := splitBaselineCPUStat(benchCPUStatData); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseMemPercentSplitBaseline(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := splitBaselineMemPercent(benchMemInfoData); err != nil {
			b.Fatal(err)
		}
	}
}
