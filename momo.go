package main

import (
    "log"
    "flag"
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
    //daemonIdPtr := flag.Int("id", 1, "Daemon id, usable to check primary affinity")
    filePathPtr := flag.String("file", "/dev/momo/null", "File path to upload")
    debugPtr := flag.Bool("debug", true, "log to stdout")
    flag.Parse()

    LogStdOut(*debugPtr)

    switch *impersonationPtr {
    case "client":
        log.Printf("*** CLIENT CODE")
        momo_client.Connect(*serverIpPtr, *portPtr, *filePathPtr)
    case "server":
        log.Printf("*** SERVER CODE")
        momo_server.Daemon(*serverIpPtr, *portPtr, *dirPtr, *replicationPtr)
    default:
        log.Println("*** ERROR: Option unknown ***")
    }

}
