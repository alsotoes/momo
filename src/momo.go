package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/client"
	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/metrics"
	"github.com/alsotoes/momo/src/server"
	"github.com/alsotoes/momo/src/transport"
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
//	-mode: Replication mode code for "repl" impersonation.
func Run() {
	impersonationPtr := flag.String("imp", "client", "Server, client, metric server or replication changer (repl) impersonation")
	serverIdPtr := flag.Int("id", -1, "Server daemon id")
	filePathPtr := flag.String("file", "/tmp/momo", "File path to upload")
	configPathPtr := flag.String("config", "conf/momo.conf", "Path to the configuration file")
	modePtr := flag.Int("mode", -1, "Replication mode to set (used with -imp repl)")
	flag.Parse()

	cfg, err := common.GetConfig(*configPathPtr)
	if err != nil {
		log.Fatalf("Failed to get config: %v", common.SanitizeLog(err.Error()))
	}

	common.LogStdOut(cfg.Global.Debug)

	if (*impersonationPtr == "server") && (*serverIdPtr >= len(cfg.Daemons) || *serverIdPtr < 0) {
		log.Fatalf("index out of range")
	}

	if *impersonationPtr == "repl" && *serverIdPtr != -1 && (*serverIdPtr >= len(cfg.Daemons) || *serverIdPtr < 0) {
		log.Fatalf("index out of range")
	}

	switch *impersonationPtr {
	case "client":
		serverId := *serverIdPtr

		// ⚡ Bolt: Implement dynamic load balancing if no serverId is specified.
		if serverId == -1 {
			fileHash, err := common.HashFile(*filePathPtr)
			if err != nil {
				log.Fatalf("Failed to hash file: %v", err)
			}

			// Build ClusterMap
			nodes := make([]*common.Node, len(cfg.Daemons))
			for i, d := range cfg.Daemons {
				nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host}
			}
			cmap := &common.ClusterMap{Nodes: nodes}

			// Calculate Primary using CRUSH
			placement, err := cmap.Placement(fileHash, 1)
			if err != nil {
				log.Fatalf("Placement failed: %v", err)
			}
			serverId = placement[0].ID
			log.Printf("Selected primary node %d for file %s", serverId, common.SanitizeLog(*filePathPtr))
		}

		if serverId >= len(cfg.Daemons) || serverId < 0 {
			log.Fatalf("index out of range")
		}
		var wg sync.WaitGroup
		wg.Add(1)
		client.Connect(&wg, cfg, *filePathPtr, serverId, common.DummyEpoch, 0, cfg.Global.ReplicationFactor)
		wg.Wait()
	case "server":
		if err := runServer(cfg, *serverIdPtr); err != nil {
			log.Fatalf("Server error: %v", common.SanitizeLog(err.Error()))
		}
	case "repl":
		if *modePtr == -1 {
			log.Fatalf("Replication mode (-mode) must be specified for 'repl' impersonation")
		}
		data := common.ReplicationData{
			New:       *modePtr,
			TimeStamp: time.Now().UnixNano(),
		}
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			log.Fatalf("Failed to marshal replication data: %v", err)
		}
		factory := transport.NewProtocolFactory(cfg)

		// ⚡ Bolt: Broadcast replication change to all nodes to ensure cluster-wide consistency.
		// In a balanced primary model, every node needs to know the latest intended mode.
		if *serverIdPtr == -1 {
			var wg sync.WaitGroup
			for i := range cfg.Daemons {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							log.Printf("CRITICAL: Panic recovered in Replication Broadcast to node %d: %v", id, r)
						}
					}()
					server.ChangeReplicationModeClient(factory, jsonBytes, id)
				}(i)
			}
			wg.Wait()
		} else {
			server.ChangeReplicationModeClient(factory, jsonBytes, *serverIdPtr)
		}
	default:
		log.Fatalf("*** ERROR: Option unknown: %s", common.SanitizeLog(*impersonationPtr))
	}
}

// runServer starts the momo server.
// It initializes the metrics collector, the replication mode change listener, and the main daemon.
// It waits for all three components to finish before shutting down.
func runServer(cfg common.Configuration, serverId int) error {
	log.Printf("*** SERVER CODE")
	now := time.Now()
	timestamp := now.UnixNano()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 3)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in Metrics Loop: %v", r)
			}
		}()
		metrics.GetMetrics(ctx, cfg, serverId)
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in Replication Server: %v", r)
			}
		}()
		errChan <- server.ChangeReplicationModeServer(ctx, cfg, serverId, timestamp)
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in Main Daemon: %v", r)
			}
		}()
		errChan <- server.Daemon(ctx, cfg, serverId)
	}()

	// Wait for any component to return an error or for the program to be interrupted
	// In a real application, we might want to catch SIGINT/SIGTERM here.
	select {
	case err := <-errChan:
		if err != nil {
			cancel() // Shut down other components
			return err
		}
	}

	return nil
}
