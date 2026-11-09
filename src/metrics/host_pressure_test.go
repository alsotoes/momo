package metrics

import (
	"errors"
	"testing"

	gopsmem "github.com/shirou/gopsutil/v3/mem"
)

type fakePressure struct {
	mem    *gopsmem.VirtualMemoryStat
	memErr error
	cpus   []float64
	cpuErr error
}

func (f fakePressure) VirtualMemory() (*gopsmem.VirtualMemoryStat, error) {
	return f.mem, f.memErr
}

func (f fakePressure) CPUPercent() ([]float64, error) { return f.cpus, f.cpuErr }

func TestHostPressureReportsBusiestCPUAndMemory(t *testing.T) {
	cpu, memUsed := HostPressure(fakePressure{
		mem:  &gopsmem.VirtualMemoryStat{UsedPercent: 63.5},
		cpus: []float64{10, 88.5, 40},
	})
	if cpu != 88.5 {
		t.Fatalf("cpu = %v, want the busiest core", cpu)
	}
	if memUsed != 63.5 {
		t.Fatalf("mem = %v", memUsed)
	}
}

func TestHostPressureReportsZeroOnErrors(t *testing.T) {
	cpu, memUsed := HostPressure(fakePressure{
		memErr: errors.New("no meminfo"),
		cpuErr: errors.New("no /proc/stat"),
	})
	if cpu != 0 || memUsed != 0 {
		t.Fatalf("failed samples must report zero, got cpu=%v mem=%v", cpu, memUsed)
	}
}
