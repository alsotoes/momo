// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

var replicationStateMutex sync.RWMutex

// currentReplicationMode is the current replication mode of the server.
var currentReplicationMode int = momo_common.ReplicationNone

// replicationState stores the old and new replication modes, and the timestamp of the last change.
var replicationState momo_common.ReplicationData

// GetReplicationState safely returns the current replicationState
func GetReplicationState() momo_common.ReplicationData {
	replicationStateMutex.RLock()
	defer replicationStateMutex.RUnlock()
	return replicationState
}

// GetCurrentReplicationMode safely returns the current currentReplicationMode
func GetCurrentReplicationMode() int {
	replicationStateMutex.RLock()
	defer replicationStateMutex.RUnlock()
	return currentReplicationMode
}

// SetReplicationState safely updates currentReplicationMode and replicationState
func SetReplicationState(newMode int, timestamp int64) momo_common.ReplicationData {
	replicationStateMutex.Lock()
	defer replicationStateMutex.Unlock()

	replicationState.Old = currentReplicationMode
	replicationState.New = newMode
	replicationState.TimeStamp = timestamp
	currentReplicationMode = newMode

	return replicationState
}

// ChangeReplicationModeServer listens for connections on a dedicated port and updates the replication mode of the server.
//
// When a client connects, it sends a JSON object containing the new replication mode.
// This function updates the server's replication mode and, if the server is the primary (serverId 0),
// it propagates the change to the other servers in the cluster.
func ChangeReplicationModeServer(ctx context.Context, cfg momo_common.Configuration, serverId int, timestamp int64) {
	daemons := cfg.Daemons
	server, err := net.Listen("tcp", daemons[serverId].ChangeReplication)
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

	log.Printf("Server changeReplicationMode started... at %s", daemons[serverId].ChangeReplication)
	log.Printf("Waiting for connections: changeReplicationMode...")
	log.Printf("default ReplicationMode value: %d", GetCurrentReplicationMode())

	// Initialize the replication state
	initialState := SetReplicationState(GetCurrentReplicationMode(), timestamp)
	replicationJson, _ := json.Marshal(initialState)
	log.Printf("ReplicationData struct: %s", string(replicationJson))

	for {
		connection, err := server.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return // Shutting down gracefully
			default:
				log.Printf("Error: %v", err)
				os.Exit(1)
			}
		}
		go func() {
			defer connection.Close()
			log.Printf("Client connected to changeReplicationMode")

			// 🛡️ Sentinel: Enforce a read/write timeout to prevent slowloris DoS attacks
			connection.SetDeadline(time.Now().Add(10 * time.Second))

			// Read and validate the AuthToken
			bufferAuthToken := make([]byte, momo_common.AuthTokenLength)
			if _, err := io.ReadFull(connection, bufferAuthToken); err != nil {
				log.Printf("Error reading AuthToken: %v", err)
				return
			}
			// 🛡️ Sentinel: Use constant-time comparison to prevent timing attacks during authentication
			expectedAuthToken := []byte(momo_common.PadString(cfg.Global.AuthToken, momo_common.AuthTokenLength))
			if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) != 1 {
				log.Printf("Invalid AuthToken received")
				return
			}

			// Decode the replication data directly from the connection
			// 🛡️ Sentinel: Limit the JSON payload size to prevent DoS via memory exhaustion
			replicationJson := momo_common.ReplicationData{}
			if err := json.NewDecoder(io.LimitReader(connection, 1024)).Decode(&replicationJson); err != nil {
				log.Printf("Failed to decode replication data: %v", err)
				return
			}

			// Update the replication state
			newState := SetReplicationState(replicationJson.New, replicationJson.TimeStamp)
			newReplicationJson, _ := json.Marshal(newState)
			log.Printf("changeReplicationMode new value: %d", replicationJson.New)
			log.Printf("ReplicationData new struct: %s", string(newReplicationJson))

			// If this is the primary server, propagate the change to the other servers
			if 0 == serverId {
				go changeReplicationModeClient(cfg.Global.AuthToken, daemons, string(newReplicationJson), 1)
				go changeReplicationModeClient(cfg.Global.AuthToken, daemons, string(newReplicationJson), 2)
			}
		}()
	}
}

// changeReplicationModeClient connects to another server in the cluster and sends the new replication mode.
// It is used by the primary server to propagate replication mode changes to the other servers.
func changeReplicationModeClient(authToken string, daemons []*momo_common.Daemon, replicationJson string, serverId int) {
	conn, err := momo_common.DialSocket(daemons[serverId].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	// Send the AuthToken first
	if _, err := conn.Write([]byte(momo_common.PadString(authToken, momo_common.AuthTokenLength))); err != nil {
		log.Printf("Failed to send AuthToken: %v", err)
		return
	}

	conn.Write([]byte(replicationJson))
	log.Printf("ReplicationData sent to serverId: %d", serverId)
}
