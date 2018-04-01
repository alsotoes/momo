package main

import (
    "log"
    "flag"
    "io/ioutil"

    momo_client "github.com/alsotoes/momo/client"
)

func LogStdOut(logApp bool) {

    if logApp {
        log.SetFlags(log.LstdFlags | log.Lmicroseconds)
    } else {
        log.SetOutput(ioutil.Discard)
    }

}

func main() {

    impersonationPtr := flag.String("imp", "client", "server or client")
    serverIpPtr := flag.String("ip", "0.0.0.0", "server ip")
    portPtr := flag.Int("port", 3333, "server port to listen for connections")
    filePathPtr := flag.String("file", "/dev/momo/null", "file path")
    debugPtr := flag.Bool("debug", true, "log to stdout")
    flag.Parse()

    LogStdOut(*debugPtr)

    switch *impersonationPtr {
    case "client":
        log.Printf("*** CLIENT CODE")
        momo_client.Connect(*serverIpPtr, *portPtr, *filePathPtr)
    case "server":
        log.Printf("*** SERVER CODE")
    default:
        log.Println("*** ERROR: Option unknown ***")
    }

}
