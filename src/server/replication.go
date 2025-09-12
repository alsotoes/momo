// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"os"

	momo_common "github.com/alsotoes/momo/src/common"
)

// CurrentReplicationMode is the current replication mode of the server.
var CurrentReplicationMode int = momo_common.ReplicationNone

// ReplicationState stores the old and new replication modes, and the timestamp of the last change.
var ReplicationState momo_common.ReplicationData

// ChangeReplicationModeServer listens for connections on a dedicated port and updates the replication mode of the server.
//
// When a client connects, it sends a JSON object containing the new replication mode.
// This function updates the server's replication mode and, if the server is the primary (serverId 0),
// it propagates the change to the other servers in the cluster.
func ChangeReplicationModeServer(daemons []*momo_common.Daemon, serverId int, timestamp int64) {
	server, err := net.Listen("tcp", daemons[serverId].ChangeReplication)
	if err != nil {
		log.Printf("Error listening: %v", err)
		os.Exit(1)
	}

	defer server.Close()
	log.Printf("Server changeReplicationMode started... at %s", daemons[serverId].ChangeReplication)
	log.Printf("Waiting for connections: changeReplicationMode...")
	log.Printf("default ReplicationMode value: %d", CurrentReplicationMode)

	// Initialize the replication state
	ReplicationState.Old = CurrentReplicationMode
	ReplicationState.New = CurrentReplicationMode
	ReplicationState.TimeStamp = timestamp
	replicationJson, _ := json.Marshal(ReplicationState)
	log.Printf("ReplicationData struct: %s", string(replicationJson))

	for {
		connection, err := server.Accept()
		if err != nil {
			log.Printf("Error: %v", err)
			os.Exit(1)
		}
		go func() {
			bufferReplicationMode := make([]byte, momo_common.FileInfoLength)
			connection.Read(bufferReplicationMode)
			log.Printf("Client connected to changeReplicationMode")

			// Decode the replication data from the connection
			replicationJson := momo_common.ReplicationData{}
			if err := json.NewDecoder(bytes.NewReader(bufferReplicationMode)).Decode(&replicationJson); err != nil {
				log.Printf("Failed to decode replication data: %v", err)
				return
			}

			// Update the replication state
			ReplicationState.Old = CurrentReplicationMode
			ReplicationState.New = replicationJson.New
			ReplicationState.TimeStamp = replicationJson.TimeStamp
			newReplicationJson, _ := json.Marshal(ReplicationState)
			CurrentReplicationMode = replicationJson.New
			log.Printf("changeReplicationMode new value: %d", replicationJson.New)
			log.Printf("ReplicationData new struct: %s", string(newReplicationJson))

			// If this is the primary server, propagate the change to the other servers
			if 0 == serverId {
				go changeReplicationModeClient(daemons, string(newReplicationJson), 1)
				go changeReplicationModeClient(daemons, string(newReplicationJson), 2)
			}
		}()
	}
}

// changeReplicationModeClient connects to another server in the cluster and sends the new replication mode.
// It is used by the primary server to propagate replication mode changes to the other servers.
func changeReplicationModeClient(daemons []*momo_common.Daemon, replicationJson string, serverId int) {
	conn, err := momo_common.DialSocket(daemons[serverId].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	conn.Write([]byte(replicationJson))
	log.Printf("ReplicationData sent to serverId: %d", serverId)
}
