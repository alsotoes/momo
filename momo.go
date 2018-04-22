package main

import (
    "log"
    "flag"
    "sync"
    "io/ioutil"

    momo_client "github.com/alsotoes/momo/client"
    momo_server "github.com/alsotoes/momo/server"
)

func LogStdOut(logApp bool) {

    if logApp {
        log.SetFlags(log.LstdFlags | log.Lmicroseconds)
    } else {
        log.SetOutput(ioutil.Discard)
    }

}

func main() {

    impersonationPtr := flag.String("imp", "client", "Server or client impersonation")
    serverIpPtr := flag.String("ip", "0.0.0.0", "Server ip")
    portPtr := flag.Int("port", 3333, "Server port to listen for connections")
    dirPtr := flag.String("dir", "./received_files/dir1/", "Path where to save the files")
    replicationPtr := flag.Int("replication", 1, "Replicaton type: 0=>no replica, 1=>chain, 2=>splay, 3=>primary splay")
    filePathPtr := flag.String("file", "/dev/momo/null", "File path to upload")
    debugPtr := flag.Bool("debug", true, "log to stdout")
    flag.Parse()

    LogStdOut(*debugPtr)

    switch *impersonationPtr {
    case "client":
        log.Printf("*** CLIENT CODE")
        var wg sync.WaitGroup
        if 3 == *replicationPtr {
            wg.Add(3)
            go momo_client.Connect(&wg, *serverIpPtr, *portPtr, *filePathPtr)
            go momo_client.Connect(&wg, "0.0.0.0", 3334, *filePathPtr)
            go momo_client.Connect(&wg, "0.0.0.0", 3335, *filePathPtr)
            wg.Wait()
        } else {
            wg.Add(1)
            momo_client.Connect(&wg, *serverIpPtr, *portPtr, *filePathPtr)
        }
    case "server":
        log.Printf("*** SERVER CODE")
        if 3 == *replicationPtr {
            momo_server.Daemon(*serverIpPtr, *portPtr, *dirPtr, 0)
        } else {
            momo_server.Daemon(*serverIpPtr, *portPtr, *dirPtr, *replicationPtr)
        }
    default:
        log.Println("*** ERROR: Option unknown ***")
    }

}
