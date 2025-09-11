package momo

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
		var wg sync.WaitGroup
		wg.Add(1)
		common.Connect(&wg, cfg.Daemons, *filePathPtr, 0, common.DummyEpoch)
		wg.Wait()
	case "server":
		runServer(cfg, *serverIdPtr)
	default:
		log.Printf("*** ERROR: Option unknown: %s", *impersonationPtr)
		os.Exit(1)
	}
}

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
