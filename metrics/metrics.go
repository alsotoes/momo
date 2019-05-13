package momo

import (
    _ "os"
    "fmt"
    _ "log"
    "time"
    _ "strings"
    _ "reflect"

    "github.com/shirou/gopsutil/mem"
    "github.com/shirou/gopsutil/cpu"
    momo_common "github.com/alsotoes/momo/common"
)

func GetMetrics(daemons []*momo_common.Daemon, serverId int, interval int) {
    for {
        // https://www.thegeekdiary.com/how-to-calculate-memory-usage-in-linux-using-sar-ps-and-free/
        // kbmemfree + kbbuffers + kbcached = actual free memory on the system
        v, _ := mem.VirtualMemory()
        memFree := (float64(v.Free) + float64(v.Buffers) + float64(v.Cached))/ float64(v.Total)
        fmt.Printf("Memory\nfreePercent: %.2f\n", memFree)
        fmt.Printf("usedPercent: %.2f\n", float64(v.UsedPercent)/100)

        fmt.Println("")

        c, _ := cpu.Percent(0, false)
        cpuFree := float64(100) - float64(c[0])
        fmt.Printf("CPU\nfreePercent: %.2f\n",cpuFree)
        fmt.Printf("usedPercent: %.2f\n",c[0])

        cpuTimes, _ := cpu.Times(false)
        fmt.Println(cpuTimes)

        fmt.Println("")
        time.Sleep(time.Duration(interval) * time.Millisecond)
    }

}
