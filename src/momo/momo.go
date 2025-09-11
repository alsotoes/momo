package momo

import (
	"flag"
	"log"
	"os"
	"sync"
	"time"

	client "github.com/alsotoes/momo/src/client"
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
		runClient(cfg, *filePathPtr)
	case "server":
		runServer(cfg, *serverIdPtr)
	default:
		log.Println("*** ERROR: Option unknown ***")
	}
}

func runClient(cfg common.Configuration, filePath string) {
	log.Printf("*** CLIENT CODE")
	var wg sync.WaitGroup
	wg.Add(1)
	client.Connect(&wg, cfg.Daemons, filePath, 0, common.DUMMY_EPOCH)
}

func runServer(cfg common.Configuration, serverId int) {
	log.Printf("*** SERVER CODE")
	now := time.Now()
	timestamp := now.UnixNano()
	go metrics.GetMetrics(cfg, serverId)
	go server.ChangeReplicationModeServer(cfg.Daemons, serverId, timestamp)
	server.Daemon(cfg.Daemons, serverId)
}
