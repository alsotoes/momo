package momo

import (
	_ "os"
    "fmt"
    _ "log"
    "time"
    _ "strings"
    "reflect"

    //"github.com/shirou/gopsutil/mem"
    //"github.com/shirou/gopsutil/cpu"
    //"github.com/shirou/gopsutil/disk_linux"
    //"github.com/shirou/gopsutil/net"
    "github.com/prometheus/node_exporter/collector"
    momo_common "github.com/alsotoes/momo/common"
)

func GetMetrics(daemons []*momo_common.Daemon, serverId int, interval int) {
    //drive := strings.Split(daemons[serverId].Drive,"/")
    for {
        diskCollector := collector.NewDiskstatsCollector
        fmt.Println(diskCollector())
        fmt.Printf("Type of a is ", *diskCollector)
        fmt.Println(reflect.TypeOf(diskCollector))

        /*
        v, _ := mem.VirtualMemory()
        fmt.Println(v)
        fmt.Println("")
        */

        /*
        c, _ := cpu.Percent(0, false)
        fmt.Println(c)
        fmt.Println("")
        */

        /*
        ct, _ := cpu.Times(false)
        fmt.Println(ct)
        fmt.Println("")
        */

        /*
        diskMetrics, _ := disk.IOCounters(daemons[serverId].Drive)
        diskIOCounters := diskMetrics[strings.Split(daemons[serverId].Drive,"/")[len(drive)-1]]
        fmt.Println(diskIOCounters)
        fmt.Println("")

		diskCounters, _ := disk.IOCountersWithContext()
        fmt.Println(diskCounters)
        //fmt.Println(diskIOCounters.ReadCount,diskIOCounters.MergedReadCount)
        //fmt.Printf("%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d\n", diskIOCounters.ReadCount,diskIOCounters.MergedReadCount,diskIOCounters.WriteCount,diskIOCounters.MergedWriteCount,diskIOCounters.ReadBytes,diskIOCounters.WriteBytes,diskIOCounters.ReadTime,diskIOCounters.WriteTime,diskIOCounters.IopsInProgress,diskIOCounters.IoTime,diskIOCounters.WeightedIO)
        */

        /*
        network, _ := net.IOCounters(false)
        fmt.Println(network)
        fmt.Println("")
        */

        time.Sleep(time.Duration(interval) * time.Millisecond)
    }

}
