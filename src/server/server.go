// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/client"
	momo_common "github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// connectToPeer is an alias for the client.Connect function, used to connect to other servers in the cluster for data replication.
var connectToPeer = client.Connect

// Daemon is the core of the momo server.
// It listens for incoming connections and handles file uploads and replication.
// The server's behavior is determined by the replicationMode, which is received from the client.
//
// The server can operate in one of the following replication modes:
//
//   - ReplicationNone: The server saves the file without replicating it to other nodes.
//   - ReplicationSplay: The primary server replicates the file to all other servers in the cluster.
//   - ReplicationChain: Servers are arranged in a chain. The primary server replicates to the next server in the chain, which then replicates to the next, and so on.
//   - ReplicationPrimarySplay: This mode is currently handled as ReplicationNone, which means no replication is performed.
//
// The replication mode is determined by the client, and for secondary servers, it's influenced by the timestamp of the operation.
func Daemon(ctx context.Context, cfg momo_common.Configuration, serverId int) error {
	daemons := cfg.Daemons
	factory := transport.NewProtocolFactory(cfg)

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

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(momo_common.PadString(cfg.Global.AuthToken, momo_common.AuthTokenLength))

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
		log.Printf("Client connected to primary Daemon")
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
	remoteAddr := momo_common.SanitizeLog(comm.RemoteAddr().String())


			// 🛡️ Sentinel: Apply a strict absolute deadline for the handshake phase to prevent Slowloris trickle attacks.
			comm.SetAbsoluteDeadline(time.Now().Add(10 * time.Second))

			defer func() {
				if success {
					log.Printf("AUDIT: Server ACK to Client %s => ACK%d", remoteAddr, serverId)
					comm.SendACK(serverId)
				}
				comm.Close()
			}()

			// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp,
			// validates the token, and returns the timestamp.
			_, ts, err := comm.HandshakeServer(expectedAuthToken)
			if err != nil {
				log.Printf("AUDIT: Handshake failed from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Add audit logging for successful authentication
			log.Printf("AUDIT: Successful authentication from %s", remoteAddr)

			// Determine the replication mode based on the server ID and timestamp
			repState := GetReplicationState()
			var finalTs int64

			if 0 == serverId {
				now := time.Now()
				finalTs = now.UnixNano()
				replicationMode = repState.New
			} else if 1 == serverId {
				finalTs = ts
				if finalTs > repState.TimeStamp {
					replicationMode = repState.New
				} else {
					replicationMode = repState.Old
				}

				if replicationMode != momo_common.ReplicationChain {
					replicationMode = momo_common.ReplicationNone
				}
			} else {
				finalTs = ts
				replicationMode = momo_common.ReplicationNone
			}

			// 🛡️ Sentinel: Ensure the replicationMode is within valid bounds.
			// If it's 0 (the uninitialized value of the enum) or otherwise invalid,
			// default to ReplicationNone to ensure the server processes the file.
			if replicationMode == 0 {
				replicationMode = momo_common.ReplicationNone
			}

			log.Printf("Cluster object global timestamp: %d", finalTs)
			log.Printf("Server Daemon replicationMode: %d", replicationMode)

			// Send the selected replication mode back to the client
			if err := comm.SendReplicationMode(replicationMode); err != nil {
				log.Printf("AUDIT: Error sending replication mode to %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Extend the absolute deadline to allow the client time to establish
			// splay connections and pre-compute file hashes before sending metadata.
			comm.SetAbsoluteDeadline(time.Now().Add(60 * time.Second))

			metadata, err := comm.ReceiveMetadata()
			if err != nil {
				log.Printf("AUDIT: Error getting metadata from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Sanitize fileName immediately to prevent path traversal in all downstream consumers.
			rawFileName := metadata.Name
			if rawFileName == "." || rawFileName == ".." || strings.Contains(rawFileName, "/") || strings.Contains(rawFileName, "\\") {
				log.Printf("AUDIT: Invalid filename received from %s: %v", remoteAddr, momo_common.SanitizeLog(rawFileName))
				return
			}
			fileName := filepath.Base(rawFileName)
			if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
				log.Printf("AUDIT: Invalid base filename received from %s: %v", remoteAddr, momo_common.SanitizeLog(fileName))
				return
			}
			metadata.Name = fileName

			// 🛡️ Sentinel: Enforce maximum file size to prevent Denial of Service via resource exhaustion
			if metadata.Size < 0 || metadata.Size > momo_common.MaxFileSize {
				log.Printf("AUDIT: Invalid file size received from %s: %d (max: %d)", remoteAddr, metadata.Size, momo_common.MaxFileSize)
				return
			}

			// 🛡️ Sentinel: Apply an absolute deadline based on file size.
			absoluteDeadline := time.Now().Add(5*time.Minute + time.Duration(metadata.Size/(10*1024*1024))*time.Minute)
			comm.SetAbsoluteDeadline(absoluteDeadline)

			var wg sync.WaitGroup

			// Handle the file based on the replication mode
			switch replicationMode {
			case momo_common.ReplicationNone, momo_common.ReplicationPrimarySplay:
				if err := getFile(comm, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
					log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
					return
				}
			case momo_common.ReplicationChain:
				if serverId == 1 {
					wg.Add(1)
					if err := getFile(comm, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
						wg.Done()
						return
					}
					connectToPeer(&wg, cfg, daemons[1].Data+"/"+metadata.Name, 2, finalTs)
					wg.Wait()
				} else {
					wg.Add(1)
					if err := getFile(comm, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
						wg.Done()
						return
					}
					connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, finalTs)
					wg.Wait()
				}
			case momo_common.ReplicationSplay:
				wg.Add(2)
				if err := getFile(comm, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
					log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, momo_common.SanitizeLog(err.Error()))
					wg.Done()
					wg.Done()
					return
				}
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("CRITICAL: Panic recovered in Splay forwarder to node 1: %v", r)
						}
					}()
					connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, finalTs)
				}()
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("CRITICAL: Panic recovered in Splay forwarder to node 2: %v", r)
						}
					}()
					connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 2, finalTs)
				}()
				wg.Wait()
			default:
				log.Printf("AUDIT: *** ERROR: Unknown replication type from %s", remoteAddr)
				return
			}
			success = true
		}(connection)
	}
}
