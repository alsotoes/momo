package momo

import (
    _ "os"
    _ "fmt"
    "log"
    "time"
    "strings"
    "strconv"
    _ "reflect"
    "encoding/json"

    "github.com/shirou/gopsutil/mem"
    "github.com/shirou/gopsutil/cpu"

    momo_common "github.com/alsotoes/momo/common"
    momo_client "github.com/alsotoes/momo/client"
)

func GetMetrics(cfg momo_common.Configuration, serverId int) {
    replicationOrder := strings.Split(cfg.Global.ReplicationOrder,",")
    momo_common.ReplicationMode, _ = strconv.Atoi(replicationOrder[0])
    index := momo_common.Contains(replicationOrder, strconv.Itoa(momo_common.ReplicationMode))
    replicationMode := momo_common.ReplicationMode

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
        cpuFree := float64(100) - float64(c[0])
        cpuUsed := c[0]

        // Change replication mode by resources metric

        // Change replication mode by timeout fallback 
        now = time.Now()
        if now.Sub(start) > (time.Duration(cfg.Metrics.FallbackInterval) * time.Millisecond) && serverId == 0 {
            index = momo_common.Contains(replicationOrder, strconv.Itoa(momo_common.ReplicationMode))
            if index != -1 && index != 0 {
                log.Printf("Replication fallback because of timeout")
                replicationMode, _ = strconv.Atoi(replicationOrder[index-1])
                pushNewReplicationMode(replicationMode)
            } else {
                log.Printf("Replication method has no fallback")
            }
            start = time.Now()
        }else {
            log.Printf(now.Sub(start).String())
            log.Printf("%.2f,%.2f,%.2f,%.2f\n",memFree, memUsed, cpuFree, cpuUsed)
        }

        /*
        cpuTimes, _ := cpu.Times(false)
        fmt.Println(cpuTimes)
        fmt.Println("")
        */
        time.Sleep(time.Duration(cfg.Metrics.Interval) * time.Millisecond)
    }
}

func pushNewReplicationMode(replication int) {
    cfg := momo_common.GetConfig()
    conn := momo_client.DialSocket(cfg.Daemons[0].Chrep)
    defer conn.Close()

    now := time.Now()
    nanos := now.UnixNano()

    replicationJsonStruct := &momo_common.ReplicationData{
        New: replication,
        TimeStamp: nanos}

    replicationJson, _ := json.Marshal(replicationJsonStruct)
    conn.Write([]byte(replicationJson))
    log.Printf("New Replication mode pushed: %s", replicationJson)
}
