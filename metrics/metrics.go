package momo

import (
    "fmt"
    _ "log"
    "time"

    "github.com/shirou/gopsutil/mem"
    "github.com/shirou/gopsutil/cpu"
    "github.com/shirou/gopsutil/disk"
)

func GetMetrics() {

    for {
        v, _ := mem.VirtualMemory()
        fmt.Println(v)

        c, _ := cpu.Percent(0, false)
        fmt.Println(c)

        ct, _ := cpu.Times(false)
        fmt.Println(ct)

        disk, _ := disk.IOCounters("/dev/sda")
        fmt.Println(disk["sda"])

        time.Sleep(500 * time.Millisecond)
    }

}
