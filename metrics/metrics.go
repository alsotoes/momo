package momo

import (
    _ "os"
    _ "fmt"
    "log"
    "time"
    "strings"
    "strconv"
    _ "reflect"

    "github.com/shirou/gopsutil/mem"
    "github.com/shirou/gopsutil/cpu"
    momo_common "github.com/alsotoes/momo/common"
)

func GetMetrics(cfg momo_common.Configuration, serverId int) {
    replicationOrder := strings.Split(cfg.Global.ReplicationOrder,",")
    momo_common.ReplicationMode, _ = strconv.Atoi(replicationOrder[0])
    log.Printf("Daemon GetMetrics stated...")

    for {
        // https://www.thegeekdiary.com/how-to-calculate-memory-usage-in-linux-using-sar-ps-and-free/
        // kbmemfree + kbbuffers + kbcached = actual free memory on the system
        v, _ := mem.VirtualMemory()
        memFree := (float64(v.Free) + float64(v.Buffers) + float64(v.Cached))/ float64(v.Total)
        memUsed := float64(v.UsedPercent)/100

        c, _ := cpu.Percent(0, false)
        cpuFree := float64(100) - float64(c[0])
        cpuUsed := c[0]

        log.Printf("%.2f,%.2f,%.2f,%.2f\n",memFree, memUsed, cpuFree, cpuUsed)

        /*
        cpuTimes, _ := cpu.Times(false)
        fmt.Println(cpuTimes)
        fmt.Println("")
        */
        time.Sleep(time.Duration(cfg.Metrics.Interval) * time.Millisecond)
    }
}
