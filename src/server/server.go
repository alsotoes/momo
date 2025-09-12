// Package server provides the core functionality for the momo server.
package server

import (
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
//	- ReplicationNone: The server saves the file without replicating it to other nodes.
//	- ReplicationSplay: The primary server replicates the file to all other servers in the cluster.
//	- ReplicationChain: Servers are arranged in a chain. The primary server replicates to the next server in the chain, which then replicates to the next, and so on.
//	- ReplicationPrimarySplay: This mode is currently handled as ReplicationNone, which means no replication is performed.
//
// The replication mode is determined by the client, and for secondary servers, it's influenced by the timestamp of the operation.
func Daemon(daemons []*momo_common.Daemon, serverId int) {
	var timestamp int64
	server, err := net.Listen("tcp", daemons[serverId].Host)
	if err != nil {
		log.Printf("Error listening: %v", err)
		os.Exit(1)
	}

	defer server.Close()
	log.Printf("Server primary Daemon started... at %s", daemons[serverId].Host)
	log.Printf("...Waiting for connections...")

	for {
		connection, err := server.Accept()
		if err != nil {
			log.Printf("Error: %v", err)
			os.Exit(1)
		}
		log.Printf("Client connected to primary Daemon")

		go func() {
			var replicationMode int
			defer func() {
				log.Printf("Server ACK to Client => ACK%d", serverId)
				connection.Write([]byte("ACK" + strconv.Itoa(serverId)))
				connection.Close()
			}()

			// Read the timestamp from the connection
			bufferTimestamp := make([]byte, momo_common.TimestampLength)
			connection.Read(bufferTimestamp)
			timestamp, err = strconv.ParseInt(string(bufferTimestamp), 10, 64)
			if err != nil {
				log.Printf("Error: %d of type %T", timestamp, timestamp)
				panic(err)
			}

			// Determine the replication mode based on the server ID and timestamp
			if 0 == serverId {
				now := time.Now()
				timestamp = now.UnixNano()
				replicationMode = ReplicationState.New
			} else if 1 == serverId {
				if timestamp > ReplicationState.TimeStamp {
					replicationMode = ReplicationState.New
				} else {
					replicationMode = ReplicationState.Old
				}

				if replicationMode != momo_common.ReplicationChain {
					replicationMode = momo_common.ReplicationNone
				}
			} else {
				replicationMode = momo_common.ReplicationNone
			}

			log.Printf("Cluster object global timestamp: %d", timestamp)
			log.Printf("Server Daemon replicationMode: %d", replicationMode)
			connection.Write([]byte(strconv.FormatInt(int64(replicationMode), 10)))

			metadata := getMetadata(connection)
			var wg sync.WaitGroup

			// Handle the file based on the replication mode
			switch replicationMode {
			case momo_common.ReplicationNone, momo_common.ReplicationPrimarySplay:
				getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
			case momo_common.ReplicationChain:
				if serverId == 1 {
					wg.Add(1)
					getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
					connectToPeer(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
					wg.Wait()
				} else {
					wg.Add(1)
					getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
					connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
					wg.Wait()
				}
			case momo_common.ReplicationSplay:
				wg.Add(2)
				getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
				go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
				go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
				wg.Wait()
			default:
				log.Println("*** ERROR: Unknown replication type")
				os.Exit(1)
			}
		}()
	}
}
