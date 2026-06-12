// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/client"
	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// connectToPeer is an alias for the client.Connect function, used to connect to other servers in the cluster for data replication.
var connectToPeer = client.Connect

// Daemon is the core of the momo server.
// It listens for incoming connections and handles file uploads and replication.
// The server's behavior is determined by the replicationMode, which is received from the client.
//
// The server can operate in one of the following replication modes:
//   - ReplicationNone: The server saves the file without replicating it to other nodes.
//   - ReplicationSplay: The primary server replicates the file to all other servers in the cluster.
//   - ReplicationChain: Servers are arranged in a chain. The primary server replicates to the next server in the chain, which then replicates to the next, and so on.
//   - ReplicationPrimarySplay: This mode is currently handled as ReplicationNone, which means no replication is performed.
//
// The replication mode is determined by the client, and for secondary servers, it's influenced by the timestamp of the operation.
func Daemon(ctx context.Context, cfg common.Configuration, serverId int) error {
	daemons := cfg.Daemons
	factory := transport.NewProtocolFactory(cfg)

	// Initialize CAS Storage
	store, err := storage.NewCASStore(daemons[serverId].Data)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer store.Close()

	server, err := factory.Listen(daemons[serverId].Host)
	if err != nil {
		return fmt.Errorf("Error listening: %v", err)
	}

	defer server.Close()

	// Handle graceful shutdown via context
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in Daemon shutdown handler: %v", r)
			}
		}()
		<-ctx.Done()
		server.Close()
	}()

	log.Printf("Server primary Daemon started... at %s using %s", daemons[serverId].Host, cfg.Global.Protocol)
	log.Printf("...Waiting for connections...")

	// 🛡️ Zero-Crash: Log a warning if the cluster cannot meet the desired durability goal.
	if cfg.Global.ReplicationFactor > len(daemons) {
		log.Printf("⚠️ WARNING: Desired replication factor (%d) exceeds available node count (%d). Data will be stored in DEGRADED mode.", cfg.Global.ReplicationFactor, len(daemons))
	}

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))

	// ⚡ Bolt: Pre-build the ClusterMap during boot to avoid per-request allocations.
	nodes := make([]*common.Node, len(cfg.Daemons))
	for i, d := range cfg.Daemons {
		nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host}
	}
	cmap := &common.ClusterMap{Nodes: nodes}

	// 🛡️ Sentinel: Enforce a limit on concurrent connections to prevent resource exhaustion (DoS).
	const maxConcurrentConnections = 1000
	sem := make(chan struct{}, maxConcurrentConnections)

	for {
		connection, err := server.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil // Shutting down gracefully
			default:
				log.Printf("Error accepting connection: %v", err)
				// 🛡️ Sentinel: Sleep briefly to prevent tight loop on transient errors (like EMFILE)
				// and avoid DoS via os.Exit(1).
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		// Acquire semaphore slot before spinning up a new goroutine
		sem <- struct{}{}
		go func(comm transport.Communicator) {
			defer func() { <-sem }()
			// 🛡️ Zero-Crash Hardening: Recover from any unexpected panics in the connection handler
			// to ensure the daemon remains stable and available for other clients.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("CRITICAL: Panic recovered in Daemon for %s: %v", comm.RemoteAddr(), r)
				}
			}()

			var replicationMode int
			var success bool

			// 🛡️ Sentinel: Capture remote address for audit logging and traceability
			remoteAddr := common.SanitizeLog(comm.RemoteAddr().String())

			// 🛡️ Sentinel: Apply a strict absolute deadline for the handshake phase to prevent Slowloris trickle attacks.
			comm.SetAbsoluteDeadline(time.Now().Add(10 * time.Second))

			defer func() {
				if success {
					log.Printf("AUDIT: Server ACK to Client %s => ACK%d", remoteAddr, serverId)
					comm.SendACK(serverId)
				}
				comm.Close()
			}()

			var ts int64
			var err error
			// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp + RequestedMode,
			// validates the token, and returns the timestamp and requested mode.
			replicationMode, ts, err = comm.HandshakeServer(expectedAuthToken)
			if err != nil {
				log.Printf("AUDIT: Handshake failed from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Add audit logging for successful authentication
			log.Printf("AUDIT: Successful authentication from %s", remoteAddr)

			// Determine the replication mode based on whether we are the Primary or a Secondary.
			// ⚡ Bolt: Use the DummyEpoch marker to identify direct client connections (Primary role).
			repState := GetReplicationState()
			var finalTs int64

			if ts == common.DummyEpoch {
				// We are the Primary for this transaction.
				now := time.Now()
				finalTs = now.UnixNano()
				// Use local state for new transactions.
				replicationMode = repState.New
				log.Printf("AUDIT: Node %d acting as Primary (Client connected)", serverId)
			} else {
				// We are a Secondary (this is a forwarded connection from another node).
				// ⚡ Bolt: Trust the requestedMode from the Primary for this specific transaction.
				finalTs = ts
				// replicationMode already contains the requestedMode from HandshakeServer.
				log.Printf("AUDIT: Node %d acting as Secondary (Primary requested mode %d)", serverId, replicationMode)
			}

			// 🛡️ Sentinel: Ensure the replicationMode is within valid bounds.
			// If it's 0 (the uninitialized value of the enum) or otherwise invalid,
			// default to ReplicationNone to ensure the server processes the file.
			if replicationMode == 0 {
				replicationMode = common.ReplicationNone
			}

			log.Printf("Cluster object global timestamp: %d", finalTs)
			log.Printf("Server Daemon replicationMode: %d", replicationMode)

			// Send the selected replication mode back to the client
			if err := comm.SendReplicationMode(replicationMode); err != nil {
				log.Printf("AUDIT: Error sending replication mode to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Extend the absolute deadline to allow the client time to establish
			// splay connections and pre-compute file hashes before sending metadata.
			comm.SetAbsoluteDeadline(time.Now().Add(60 * time.Second))

			metadata, err := comm.ReceiveMetadata()
			if err != nil {
				log.Printf("AUDIT: Error getting metadata from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Zero-Crash: Defensive check for storage initialization.
			if store == nil {
				log.Printf("AUDIT: Storage error for %s: store not initialized: %v", remoteAddr, syscall.EIO)
				return
			}

			// ⚡ Bolt: Content-Addressable Deduplication Check.
			exists, err := store.Has(metadata.Hash)
			if err != nil {
				log.Printf("AUDIT: Storage error checking hash %s: %v", metadata.Hash, common.SanitizeLog(err.Error()))
				return
			}

			if exists {
				log.Printf("AUDIT: Deduplication hit for %s (hash: %s)", remoteAddr, metadata.Hash)
				if err := comm.SendMetadataStatus(transport.MetadataStatusSkipPayload); err != nil {
					log.Printf("AUDIT: Error sending metadata status to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
					return
				}
			} else {
				if err := comm.SendMetadataStatus(transport.MetadataStatusSendPayload); err != nil {
					log.Printf("AUDIT: Error sending metadata status to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
					return
				}
			}

			// 🛡️ Sentinel: Sanitize fileName immediately to prevent path traversal in all downstream consumers.
			rawFileName := metadata.Name
			if rawFileName == "" || rawFileName == "." || rawFileName == ".." || strings.Contains(rawFileName, "/") || strings.Contains(rawFileName, "\\") {
				log.Printf("AUDIT: Invalid filename received from %s: %v", remoteAddr, common.SanitizeLog(rawFileName))
				return
			}
			fileName := filepath.Base(rawFileName)
			if fileName == "" || fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
				log.Printf("AUDIT: Invalid filename received from %s: %v", remoteAddr, common.SanitizeLog(fileName))
				return
			}

			// 🛡️ Sentinel: Enforce maximum file size to prevent Denial of Service via resource exhaustion
			if metadata.Size < 0 || metadata.Size > common.MaxFileSize {
				log.Printf("AUDIT: Invalid file size received from %s: %d (max: %d)", remoteAddr, metadata.Size, common.MaxFileSize)
				return
			}

			// Calculate Placement using CRUSH
			factor := cfg.Global.ReplicationFactor
			if replicationMode == common.ReplicationNone {
				factor = 1
			}

			// Get all nodes in the preferred order for this hash using the pre-built cmap.
			placement, err := cmap.Placement(metadata.Hash, factor)
			if err != nil {
				log.Printf("AUDIT: Placement failed for %s: %v", metadata.Hash, err)
				return
			}

			// 🛡️ Sentinel: Apply an absolute deadline based on file size.
			absoluteDeadline := time.Now().Add(5*time.Minute + time.Duration(metadata.Size/(10*1024*1024))*time.Minute)
			comm.SetAbsoluteDeadline(absoluteDeadline)

			var wg sync.WaitGroup

			// Handle the file based on the replication mode
			switch replicationMode {
			case common.ReplicationNone, common.ReplicationPrimarySplay:
				if exists {
					// ⚡ Bolt: Deduplication hit. Just update metadata mapping without reading payload.
					if err := store.Put(fileName, metadata.Hash, metadata.Size, nil); err != nil {
						log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
					}
				} else {
					if err := getFile(comm, store, fileName, metadata.Hash, metadata.Size); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
						return
					}
				}
			case common.ReplicationChain:
				// In Chain mode, we find our position in the placement list and forward to the next node.
				myPos := -1
				for i, n := range placement {
					if n.ID == serverId {
						myPos = i
						break
					}
				}

				wg.Add(1)
				if exists {
					// ⚡ Bolt: Deduplication hit. Just update metadata mapping without reading payload.
					if err := store.Put(fileName, metadata.Hash, metadata.Size, nil); err != nil {
						log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
					}
				} else {
					if err := getFile(comm, store, fileName, metadata.Hash, metadata.Size); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
						wg.Done()
						return
					}
				}

				if myPos != -1 && myPos < len(placement)-1 {
					nextHop := placement[myPos+1]
					blobPath, _ := store.GetBlobPath(fileName)
					log.Printf("AUDIT: Chain forwarding from Node %d to Node %d", serverId, nextHop.ID)
					
					// 🛡️ Zero-Crash: Wrap Chain forwarding in a goroutine with recovery for consistency and safety.
					go func(id int, path string) {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("CRITICAL: Panic recovered in Chain forwarder to node %d: %v", id, r)
							}
						}()
						// ⚡ Bolt: connectToPeer (client.Connect) handles wg.Done() internally via defer.
						connectToPeer(&wg, cfg, path, id, finalTs, replicationMode, factor)
					}(nextHop.ID, blobPath)
				} else {
					wg.Done()
				}
				wg.Wait()

			case common.ReplicationSplay:
				// In Splay mode, the primary (first node in placement) forwards to all others.
				if placement[0].ID == serverId {
					wg.Add(len(placement) - 1)
					if exists {
						// ⚡ Bolt: Deduplication hit. Just update metadata mapping.
						if err := store.Put(fileName, metadata.Hash, metadata.Size, nil); err != nil {
							log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
						}
					} else {
						if err := getFile(comm, store, fileName, metadata.Hash, metadata.Size); err != nil {
							log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
							for i := 0; i < len(placement)-1; i++ {
								wg.Done()
							}
							return
						}
					}
					blobPath, _ := store.GetBlobPath(fileName)
					for i := 1; i < len(placement); i++ {
						targetId := placement[i].ID
						go func(id int) {
							// ⚡ Bolt: connectToPeer (client.Connect) handles wg.Done() internally via defer.
							// Wait, if connectToPeer handles wg.Done(), we MUST NOT call it here.
							// client.Connect DOES call wg.Done().
							defer func() {
								if r := recover(); r != nil {
									log.Printf("CRITICAL: Panic recovered in Splay forwarder to node %d: %v", id, r)
								}
							}()
							connectToPeer(&wg, cfg, blobPath, id, finalTs, replicationMode, factor)
						}(targetId)
					}
					wg.Wait()
				} else {
					// We are a secondary in a splay, just receive the file if needed.
					if exists {
						if err := store.Put(fileName, metadata.Hash, metadata.Size, nil); err != nil {
							log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
						}
					} else {
						if err := getFile(comm, store, fileName, metadata.Hash, metadata.Size); err != nil {
							log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
							return
						}
					}
				}
			default:
				log.Printf("AUDIT: *** ERROR: Unknown replication type from %s", remoteAddr)
				return
			}
			success = true
		}(connection)
	}
}
