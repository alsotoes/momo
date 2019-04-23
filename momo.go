package main

import (
    "log"
    _"fmt"
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
    serverIdPtr := flag.Int("id", 0, "Server daemon id")
    filePathPtr := flag.String("file", "/dev/momo/null", "File path to upload")
    flag.Parse()

    cfg := momo_common.GetConfig()
    momo_common.LogStdOut(cfg.Debug)

    //fmt.Println("host: " + cfg.Daemons[0].Host)

    // Important:
    //  Affinity work in this order [0,1,2] thats why a lot of lines are bonded to ServerId 0
    //  ServerId 0 choose and change replication.

    switch *impersonationPtr {
        case "client":
            log.Printf("*** CLIENT CODE")
            var wg sync.WaitGroup
            wg.Add(1)
            momo_client.Connect(&wg, cfg.Daemons, *filePathPtr, 0)
        case "server":
            log.Printf("*** SERVER CODE")
            go momo_server.ChangeReplicationMode(cfg.Daemons[*serverIdPtr].Chrep)
            momo_server.Daemon(cfg.Daemons, *serverIdPtr)
        case "metric":
            log.Printf("*** METRIC CODE")
            momo_metrics.GetMetrics(cfg.MetricsInterval)
        default:
            log.Println("*** ERROR: Option unknown ***")
    }
}
