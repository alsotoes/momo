package main

import (
    "log"
    _ "fmt"
    "flag"
    "sync"
    _ "strconv"
    _ "io/ioutil"

    momo_client "github.com/alsotoes/momo/client"
    momo_server "github.com/alsotoes/momo/server"
    momo_common "github.com/alsotoes/momo/common"
    momo_metrics "github.com/alsotoes/momo/metrics"
)

func main() {

    impersonationPtr := flag.String("imp", "client", "Server, client or metric server impersonation")
    serverIpPtr := flag.String("ip", "0.0.0.0", "Server ip")
    portPtr := flag.Int("port", 3333, "Server port to listen for connections")
    metricsPtr := flag.String("metric", "0.0.0.0:3323", "Server metric to change replicationMode")
    dirPtr := flag.String("dir", "./received_files/dir1/", "Path where to save the files")
    replicationPtr := flag.Int("replication", 1, "Replicaton type: 0=>no replica, 1=>chain, 2=>splay, 3=>primary splay")
    filePathPtr := flag.String("file", "/dev/momo/null", "File path to upload")
    flag.Parse()

    cfg := momo_common.GetConfig()
    momo_common.LogStdOut(cfg.Debug)

    /*
    fmt.Println(len(cfg.Daemons))
    for i := range(cfg.Daemons) {
        daemon := cfg.Daemons[i]
        fmt.Println("host: " + daemon.Host)
        fmt.Println("metric: " + daemon.Metric)
        fmt.Println("data: " + daemon.Data)
    }
    */

    switch *impersonationPtr {
    case "client":
        log.Printf("*** CLIENT CODE")
        var wg sync.WaitGroup
        wg.Add(1)
        momo_client.Connect(&wg, *serverIpPtr, *portPtr, *filePathPtr)
    case "server":
        log.Printf("*** SERVER CODE")
        go momo_server.ChangeReplicationMode(*metricsPtr)
        momo_server.Daemon(*serverIpPtr, *portPtr, *dirPtr, *replicationPtr)
    case "metric":
        log.Printf("*** METRIC CODE")
        momo_metrics.GetMetrics()
    default:
        log.Println("*** ERROR: Option unknown ***")
    }

}
