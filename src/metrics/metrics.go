package momo

import (
    "log"
    "time"
    "strings"
    "strconv"

    "github.com/shirou/gopsutil/v3/mem"
    "github.com/shirou/gopsutil/v3/cpu"

    momo_common "github.com/alsotoes/momo/src/common"
)

func GetMetrics(cfg momo_common.Configuration, serverId int) {
    var index int
    replicationOrder := strings.Split(cfg.Global.ReplicationOrder,",")
    momo_common.ReplicationMode, _ = strconv.Atoi(replicationOrder[0])
    replicationMode := momo_common.ReplicationMode

    if serverId != 0 {
        return
    }

    log.Printf("Daemon GetMetrics stated...")
    start := time.Now()
    now := time.Now()

    for {
        // https://www.thegeekdiary.com/how-to-calculate-memory-usage-in-linux-using-sar-ps-and-free/
        // kbmemfree + kbbuffers + kbcached = actual free memory on the system
        v, _ := mem.VirtualMemory()
        memFree := (float64(v.Free) + float64(v.Buffers) + float64(v.Cached))/ float64(v.Total)
        memUsed := float64(v.UsedPercent)/100

        c, _ := cpu.Percent(0, false)
        cpuFree := (float64(100) - float64(c[0]))/100
        cpuUsed := c[0]/100

        if true == cfg.Global.PolymorphicSystem {
            index = momo_common.Contains(replicationOrder, strconv.Itoa(momo_common.ReplicationMode))
            // Change replication mode by resources metric
            if index >= 0 && index < len(replicationOrder) {
                if memFree <= cfg.Metrics.MinThreshold || cpuFree <= cfg.Metrics.MinThreshold {
                    if index > 0 {
                        log.Printf("Replication changed because cfg.Metrics.MinThreshold reached")
                        replicationMode, _ = strconv.Atoi(replicationOrder[index-1])
                        pushNewReplicationMode(replicationMode)
                        start = time.Now()
                    }
                }
                if memUsed >= cfg.Metrics.MaxThreshold || cpuUsed >= cfg.Metrics.MaxThreshold {
                    if index < len(replicationOrder) {
                        log.Printf("Replication changed because cfg.Metrics.MaxThreshold reached")
                        replicationMode, _ = strconv.Atoi(replicationOrder[index+1])
                        pushNewReplicationMode(replicationMode)
                        start = time.Now()
                    }
                }
            }

            // Change replication mode by timeout fallback 
            now = time.Now()
            if now.Sub(start) > (time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond) && serverId == 0 {
                if index != -1 && index != 0 {
                    log.Printf("Replication fallback because of timeout")
                    replicationMode, _ = strconv.Atoi(replicationOrder[index-1])
                    pushNewReplicationMode(replicationMode)
                } else {
                    log.Printf("Replication method has no fallback")
                }
                start = time.Now()
            }

            /*
            cpuTimes, _ := cpu.Times(false)
            fmt.Println(cpuTimes)
            fmt.Println("")
            */
            time.Sleep(time.Duration(cfg.Metrics.Interval) * time.Millisecond)
        } else {
            log.Printf("Replication will not change beacuse polymorphic_system is set to false")
            return
        }
    }
}
