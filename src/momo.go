package main

import (
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/common"
	metrics "github.com/alsotoes/momo/src/metrics"
	server "github.com/alsotoes/momo/src/server"
)

func main() {
	Run()
}

// Run is the main entry point for the momo application.
// It parses command-line flags to determine whether to run in client or server mode.
//
// In client mode, it connects to the server and uploads a file.
// In server mode, it starts the server, which listens for incoming connections
// and handles file uploads and replication.
//
// The following command-line flags are available:
//
//	-imp: Server, client or metric server impersonation (default: "client").
//	-id: Server daemon id (default: -1).
//	-file: File path to upload (default: "/tmp/momo").
//	-config: Path to the configuration file (default: "conf/momo.conf").
func Run() {
	impersonationPtr := flag.String("imp", "client", "Server, client or metric server impersonation")
	serverIdPtr := flag.Int("id", -1, "Server daemon id")
	filePathPtr := flag.String("file", "/tmp/momo", "File path to upload")
	configPathPtr := flag.String("config", "conf/momo.conf", "Path to the configuration file")
	flag.Parse()

	cfg, err := common.GetConfig(*configPathPtr)
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}

	common.LogStdOut(cfg.Global.Debug)

	if *impersonationPtr == "server" && (*serverIdPtr >= len(cfg.Daemons) || *serverIdPtr < 0) {
		log.Printf("panic: index out of range")
		os.Exit(1)
	}

	switch *impersonationPtr {
	case "client":
		serverId := 0
		if *serverIdPtr != -1 {
			serverId = *serverIdPtr
		}
		if serverId >= len(cfg.Daemons) || serverId < 0 {
			log.Printf("panic: index out of range")
			os.Exit(1)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		common.Connect(&wg, cfg.Daemons, *filePathPtr, serverId, common.DummyEpoch)
		wg.Wait()
	case "server":
		runServer(cfg, *serverIdPtr)
	default:
		log.Printf("*** ERROR: Option unknown: %s", *impersonationPtr)
		os.Exit(1)
	}
}

// runServer starts the momo server.
// It initializes the metrics collector, the replication mode change listener, and the main daemon.
// It waits for all three components to finish before shutting down.
func runServer(cfg common.Configuration, serverId int) {
	log.Printf("*** SERVER CODE")
	now := time.Now()
	timestamp := now.UnixNano()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		metrics.GetMetrics(cfg, serverId)
	}()

	go func() {
		defer wg.Done()
		server.ChangeReplicationModeServer(cfg.Daemons, serverId, timestamp)
	}()

	go func() {
		defer wg.Done()
		server.Daemon(cfg.Daemons, serverId)
	}()

	wg.Wait()
	log.Printf("Server shutting down")
}
