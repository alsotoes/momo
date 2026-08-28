package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/client"
	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/metrics"
	"github.com/alsotoes/momo/src/momofs"
	"github.com/alsotoes/momo/src/server"
	"github.com/alsotoes/momo/src/storage"
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
	remotePathPtr := flag.String("remote-path", "", "Remote virtual directory path to upload the file to")
	e2eeKeyPtr := flag.String("e2ee-key", "", "64-hex 256-bit E2EE master key for envelope encrypt/decrypt (client-held, never shared with the server); applies to native client mode and the S3 s3enc/s3dec impersonations")
	e2eeKeyIDPtr := flag.String("e2ee-key-id", "", "Key identifier stored in the envelope (default: \"default\")")
	outPathPtr := flag.String("out", "", "Output path for -imp s3enc / s3dec")
	mountPointPtr := flag.String("fs-mount", "", "Mount point for -imp fs (momofs FUSE)")
	fsDataPtr := flag.String("fs-data", "", "Optional data directory override for -imp fs (default: daemon data dir from config)")
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
				nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host, Domain: d.FailureDomain}
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
		// Envelope E2EE (zero-trust): client-held key for the native protocol.
		// This overrides config values so the CLI flag is the source of truth;
		// the key never leaves the client and is never persisted to the server.
		if *e2eeKeyPtr != "" {
			cfg.Global.E2EEKey = *e2eeKeyPtr
		}
		if *e2eeKeyIDPtr != "" {
			cfg.Global.E2EEKeyID = *e2eeKeyIDPtr
		}
		var wg sync.WaitGroup
		wg.Add(1)
		client.Connect(&wg, cfg, *filePathPtr, *remotePathPtr, serverId, time.Now().UnixNano(), 0, cfg.Global.ReplicationFactor)
		wg.Wait()
	case "server":
		if err := runServer(context.Background(), cfg, *serverIdPtr); err != nil {
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
	case "s3enc", "s3dec":
		if err := runS3Envelope(*impersonationPtr, *filePathPtr, *outPathPtr, *e2eeKeyPtr, *e2eeKeyIDPtr); err != nil {
			log.Fatalf("S3 envelope error: %v", common.SanitizeLog(err.Error()))
		}
	case "fs":
		if err := runFuseMount(cfg, *serverIdPtr, *mountPointPtr, *fsDataPtr); err != nil {
			log.Fatalf("momofs mount error: %v", common.SanitizeLog(err.Error()))
		}
	default:
		log.Fatalf("*** ERROR: Option unknown: %s", common.SanitizeLog(*impersonationPtr))
	}
}

// runFuseMount mounts the momofs FUSE filesystem (R4, #932) at mountpoint,
// backed by the configured storage for the given daemon. It serves until the
// process receives SIGINT/SIGTERM, then unmounts and exits.
func runFuseMount(cfg common.Configuration, serverId int, mountPoint, dataDirOverride string) error {
	if mountPoint == "" {
		return fmt.Errorf("-fs-mount (mount point) is required for -imp fs: %w", syscall.EINVAL)
	}
	if serverId == -1 {
		serverId = 0
	}
	if serverId >= len(cfg.Daemons) || serverId < 0 {
		return fmt.Errorf("index out of range: serverId %d (have %d daemons): %w", serverId, len(cfg.Daemons), syscall.EINVAL)
	}

	daemon := *cfg.Daemons[serverId]
	if dataDirOverride != "" {
		daemon.Data = dataDirOverride
	}

	encKeyHex := ""
	if cfg.Global.EncryptionEnabled {
		masterKey, decErr := hex.DecodeString(cfg.Global.EncryptionKey)
		if decErr != nil {
			return fmt.Errorf("failed to decode encryption key: %v: %w", decErr, syscall.EINVAL)
		}
		atRestKey, kErr := momocrypto.DeriveKey(masterKey, cfg.Global.EncryptionTenant, momocrypto.DomainAtRest)
		if kErr != nil {
			return fmt.Errorf("failed to derive at-rest key: %w", kErr)
		}
		encKeyHex = hex.EncodeToString(atRestKey)
	}
	store, err := storage.NewStore(cfg.Storage, &daemon, encKeyHex)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("momofs: mounting FUSE at %s (id %d)", mountPoint, serverId)
	if err := momofs.ServeFUSE(ctx, mountPoint, store); err != nil {
		return err
	}
	return nil
}

// runS3Envelope implements the -imp s3enc and -imp s3dec modes: a client-side
// envelope-encryption helper for the S3 gateway (issue #777). It encrypts a
// local file into a self-describing momo E2EE envelope object (for upload via
// any S3 client to the momo gateway) or decrypts such an envelope back to
// plaintext. The master key is client-held only; it must never be the server's
// EncryptionKey.
func runS3Envelope(mode, filePath, outPath, e2eeKey, keyID string) error {
	if e2eeKey == "" {
		return fmt.Errorf("s3 %s requires -e2ee-key (64 hex characters): %w", mode, syscall.EINVAL)
	}
	if len(e2eeKey) != momocrypto.MaxKeyHexSize {
		return fmt.Errorf("s3 %s requires a 64-hex 256-bit -e2ee-key: %w", mode, syscall.EINVAL)
	}
	masterKey, err := hex.DecodeString(e2eeKey)
	if err != nil {
		return fmt.Errorf("failed to decode -e2ee-key: %w", err)
	}
	if keyID == "" {
		keyID = "default"
	}

	in, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer in.Close()

	out := io.Writer(os.Stdout)
	var outFile *os.File
	if outPath != "" {
		outFile, err = os.Create(outPath)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", outPath, err)
		}
		defer outFile.Close()
		out = outFile
	}

	switch mode {
	case "s3enc":
		if err := momocrypto.EncryptEnvelope(out, in, masterKey, keyID); err != nil {
			return fmt.Errorf("failed to encrypt envelope: %w", err)
		}
	case "s3dec":
		if _, err := momocrypto.DecryptEnvelope(in, out, masterKey); err != nil {
			return fmt.Errorf("failed to decrypt envelope: %w", err)
		}
	}
	if outFile != nil {
		if err := outFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync %s: %w", outPath, err)
		}
	}
	return nil
}

// runMetricsLoop runs the metrics loop with panic recovery.
func runMetricsLoop(ctx context.Context, cfg common.Configuration, serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Metrics Loop: %v", r)
			err = fmt.Errorf("metrics loop panic: %w", syscall.EINVAL)
		}
	}()
	metrics.GetMetrics(ctx, cfg, serverId)
	return nil
}

// runReplicationServer runs the replication server with panic recovery.
func runReplicationServer(ctx context.Context, cfg common.Configuration, serverId int, timestamp int64) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Replication Server: %v", r)
			err = fmt.Errorf("replication server panic: %w", syscall.ENETDOWN)
		}
	}()
	return server.ChangeReplicationModeServer(ctx, cfg, serverId, timestamp)
}

// runMainDaemon runs the main server daemon with panic recovery.
func runMainDaemon(ctx context.Context, cfg common.Configuration, serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Main Daemon: %v", r)
			err = fmt.Errorf("main daemon panic: %w", syscall.EIO)
		}
	}()
	return server.Daemon(ctx, cfg, serverId)
}

// runServer starts the momo server.
// It initializes the metrics collector, the replication mode change listener, and the main daemon.
// It waits for all three components to finish before shutting down.
func runServer(ctx context.Context, cfg common.Configuration, serverId int) (err error) {
	log.Printf("*** SERVER CODE")
	now := time.Now()
	timestamp := now.UnixNano()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in runServer: %v", r)
			err = fmt.Errorf("runServer panic: %w", syscall.EIO)
		}
	}()

	errChan := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		if e := runMetricsLoop(ctx, cfg, serverId); e != nil {
			errChan <- e
		}
	}()

	go func() {
		defer wg.Done()
		errChan <- runReplicationServer(ctx, cfg, serverId, timestamp)
	}()

	go func() {
		defer wg.Done()
		errChan <- runMainDaemon(ctx, cfg, serverId)
	}()

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var firstErr error
	for e := range errChan {
		if e != nil && firstErr == nil {
			firstErr = e
			cancel()
		}
	}

	return firstErr
}
