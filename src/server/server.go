// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// connectToPeer is an alias for the momo_common.Connect function, used to connect to other servers in the cluster for data replication.
var connectToPeer = momo_common.Connect

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
	var timestamp int64
	server, err := net.Listen("tcp", daemons[serverId].Host)
	if err != nil {
		return fmt.Errorf("Error listening: %v", err)
	}

	defer server.Close()

	// Handle graceful shutdown via context
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	log.Printf("Server primary Daemon started... at %s", daemons[serverId].Host)
	log.Printf("...Waiting for connections...")

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(momo_common.PadString(cfg.Global.AuthToken, momo_common.AuthTokenLength))

	// 🛡️ Sentinel: Enforce a limit on concurrent connections to prevent resource exhaustion (DoS).
	// Without this limit, an attacker could open unbounded connections, crashing the server via OOM or FD exhaustion.
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

		go func() {
			defer func() { <-sem }() // Release semaphore slot when done
			var replicationMode int
			var success bool

			// 🛡️ Sentinel: Use an idle timeout to prevent Slowloris attacks without breaking large file uploads
			idleConn := momo_common.NewIdleTimeoutConn(connection, 30*time.Second)

			defer func() {
				if success {
					log.Printf("Server ACK to Client => ACK%d", serverId)
					// ⚡ Bolt: Avoid string allocations during formatting by using a stack-allocated buffer
					var ackBuf [32]byte
					idleConn.Write(strconv.AppendInt(append(ackBuf[:0], "ACK"...), int64(serverId), 10))
				}
				idleConn.Close()
			}()

			// Read the AuthToken and timestamp from the connection in a single call
			// ⚡ Bolt: Combine reads into a single buffer to reduce system calls and improve performance.
			var handshakeBuf [momo_common.AuthTokenLength + momo_common.TimestampLength]byte
			if _, err := io.ReadFull(idleConn, handshakeBuf[:]); err != nil {
				log.Printf("Error reading handshake: %v", err)
				return
			}

			bufferAuthToken := handshakeBuf[:momo_common.AuthTokenLength]
			bufferTimestamp := handshakeBuf[momo_common.AuthTokenLength:]

			// 🛡️ Sentinel: Use constant-time comparison to prevent timing attacks during authentication
			if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) != 1 {
				log.Printf("Invalid AuthToken received: %v", syscall.EACCES)
				return
			}

			// ⚡ Bolt: Parse timestamp directly from byte slice to avoid allocation
			timestamp, err = parsePaddedIntFast(bufferTimestamp)
			if err != nil {
				log.Printf("Error parsing timestamp: %v", err)
				return
			}

			// Determine the replication mode based on the server ID and timestamp
			repState := GetReplicationState()
			if 0 == serverId {
				now := time.Now()
				timestamp = now.UnixNano()
				replicationMode = repState.New
			} else if 1 == serverId {
				if timestamp > repState.TimeStamp {
					replicationMode = repState.New
				} else {
					replicationMode = repState.Old
				}

				if replicationMode != momo_common.ReplicationChain {
					replicationMode = momo_common.ReplicationNone
				}
			} else {
				replicationMode = momo_common.ReplicationNone
			}

			log.Printf("Cluster object global timestamp: %d", timestamp)
			log.Printf("Server Daemon replicationMode: %d", replicationMode)
			// ⚡ Bolt: Avoid string allocations during formatting by using a stack-allocated buffer
			var repModeBuf [16]byte
			if _, err := idleConn.Write(strconv.AppendInt(repModeBuf[:0], int64(replicationMode), 10)); err != nil {
				log.Printf("Error sending replication mode: %v", err)
				return
			}

			metadata, err := getMetadata(idleConn)
			if err != nil {
				log.Printf("Error getting metadata: %v", err)
				return
			}

			// 🛡️ Sentinel: Apply an absolute deadline to prevent Slowloris-style trickle attacks
			// during the actual file transfer. Base the deadline on a generous estimate: 5 minutes + 1 minute per 10MB.
			absoluteDeadline := time.Now().Add(5*time.Minute + time.Duration(metadata.Size/(10*1024*1024))*time.Minute)
			idleConn.SetAbsoluteDeadline(absoluteDeadline)

			var wg sync.WaitGroup

			// Handle the file based on the replication mode
			switch replicationMode {
			case momo_common.ReplicationNone, momo_common.ReplicationPrimarySplay:
				if err := getFile(idleConn, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
					log.Printf("Error getting file: %v", err)
					return
				}
			case momo_common.ReplicationChain:
				if serverId == 1 {
					wg.Add(1)
					if err := getFile(idleConn, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
						log.Printf("Error getting file: %v", err)
						wg.Done()
						return
					}
					connectToPeer(&wg, cfg, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
					wg.Wait()
				} else {
					wg.Add(1)
					if err := getFile(idleConn, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
						log.Printf("Error getting file: %v", err)
						wg.Done()
						return
					}
					connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
					wg.Wait()
				}
			case momo_common.ReplicationSplay:
				wg.Add(2)
				if err := getFile(idleConn, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
					log.Printf("Error getting file: %v", err)
					wg.Done() // Need to handle waitgroup correctly if one fails
					wg.Done()
					return
				}
				go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
				go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
				wg.Wait()
			default:
				log.Println("*** ERROR: Unknown replication type")
				return
			}
			success = true
		}()
	}
}
