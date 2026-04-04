// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
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
func Daemon(ctx context.Context, daemons []*momo_common.Daemon, serverId int) {
	var timestamp int64
	server, err := net.Listen("tcp", daemons[serverId].Host)
	if err != nil {
		log.Printf("Error listening: %v", err)
		os.Exit(1)
	}

	defer server.Close()

	// Handle graceful shutdown via context
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	log.Printf("Server primary Daemon started... at %s", daemons[serverId].Host)
	log.Printf("...Waiting for connections...")

	for {
		connection, err := server.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return // Shutting down gracefully
			default:
				// 🛡️ Sentinel: Do not exit on Accept errors (e.g. EMFILE) to prevent Denial of Service.
				log.Printf("Error accepting connection: %v", err)
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		log.Printf("Client connected to primary Daemon")

		go func() {
			var replicationMode int
			var success bool

			// 🛡️ Sentinel: Use an idle timeout to prevent Slowloris attacks without breaking large file uploads
			idleConn := momo_common.NewIdleTimeoutConn(connection, 30*time.Second)

			defer func() {
				if success {
					log.Printf("Server ACK to Client => ACK%d", serverId)
					// ⚡ Bolt: Use strconv.AppendInt directly on the "ACK" byte slice
					// to avoid string concatenation ("ACK" + string) overhead before network write.
					idleConn.Write(strconv.AppendInt([]byte("ACK"), int64(serverId), 10))
				}
				idleConn.Close()
			}()

			// Read the timestamp from the connection
			bufferTimestamp := make([]byte, momo_common.TimestampLength)
			if _, err := io.ReadFull(idleConn, bufferTimestamp); err != nil {
				log.Printf("Error reading timestamp: %v", err)
				return
			}
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
			// ⚡ Bolt: Use strconv.AppendInt instead of []byte(strconv.FormatInt())
			// to avoid intermediate string allocations when formatting integers into byte slices for network transmission.
			if _, err := idleConn.Write(strconv.AppendInt(make([]byte, 0, 32), int64(replicationMode), 10)); err != nil {
				log.Printf("Error sending replication mode: %v", err)
				return
			}

			metadata, err := getMetadata(idleConn)
			if err != nil {
				log.Printf("Error getting metadata: %v", err)
				return
			}
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
					connectToPeer(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
					wg.Wait()
				} else {
					wg.Add(1)
					if err := getFile(idleConn, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size); err != nil {
						log.Printf("Error getting file: %v", err)
						wg.Done()
						return
					}
					connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
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
				go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
				go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
				wg.Wait()
			default:
				log.Println("*** ERROR: Unknown replication type")
				return
			}
			success = true
		}()
	}
}
