package main

import (
    "os"
    "log"
    "flag"
    "sync"
    "time"

    momo_client "github.com/alsotoes/momo/client"
    momo_server "github.com/alsotoes/momo/server"
    momo_common "github.com/alsotoes/momo/common"
    momo_metrics "github.com/alsotoes/momo/metrics"
)

func main() {
    impersonationPtr := flag.String("imp", "client", "Server, client or metric server impersonation")
    serverIdPtr := flag.Int("id", -1, "Server daemon id")
    filePathPtr := flag.String("file", "/tmp/momo", "File path to upload")
    flag.Parse()

    cfg := momo_common.GetConfig()
    momo_common.LogStdOut(cfg.Global.Debug)

    if *impersonationPtr == "server" && (*serverIdPtr >= len(cfg.Daemons) || *serverIdPtr < 0) {
        log.Printf("panic: index out of range")
        os.Exit(1)
    }

    // Important:
    //  Affinity work in this order [0,1,2] thats why a lot of lines are bonded to ServerId 0
    //  ServerId 0 choose and change replication.

    switch *impersonationPtr {
        case "client":
            log.Printf("*** CLIENT CODE")
            var wg sync.WaitGroup
            wg.Add(1)
            momo_client.Connect(&wg, cfg.Daemons, *filePathPtr, 0, momo_common.DUMMY_EPOCH)
        case "server":
            log.Printf("*** SERVER CODE")
            now := time.Now()
            timestamp := now.UnixNano()
            go momo_metrics.GetMetrics(cfg, *serverIdPtr)
            go momo_server.ChangeReplicationModeServer(cfg.Daemons, *serverIdPtr, timestamp)
            momo_server.Daemon(cfg.Daemons, *serverIdPtr)
        default:
            log.Println("*** ERROR: Option unknown ***")
    }
}
