// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// payloadPool provides reusable byte slices for replication broadcasts to reduce allocations.
var payloadPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024)
	},
}

var replicationStateMutex sync.RWMutex

// currentReplicationMode is the current replication mode of the server.
var currentReplicationMode int = common.ReplicationNone

// replicationState stores the old and new replication modes, and the timestamp of the last change.
var replicationState common.ReplicationData

// GetReplicationState safely returns the current replicationState
func GetReplicationState() common.ReplicationData {
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
func SetReplicationState(newMode int, timestamp int64) common.ReplicationData {
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
func ChangeReplicationModeServer(ctx context.Context, cfg common.Configuration, serverId int, timestamp int64) error {
	daemons := cfg.Daemons
	factory := transport.NewProtocolFactory(cfg)
	server, err := factory.Listen(daemons[serverId].ChangeReplication)
	if err != nil {
		return fmt.Errorf("Error listening: %v", err)
	}

	defer server.Close()

	// Handle graceful shutdown via context
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in ChangeReplicationMode shutdown handler: %v", r)
			}
		}()
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

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))

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
				log.Printf("Error accepting connection: %v", common.SanitizeLog(err.Error()))
				// 🛡️ Sentinel: Sleep briefly to prevent tight loop on transient errors (like EMFILE)
				// and avoid DoS via os.Exit(1).
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		// Acquire semaphore slot before spinning up a new goroutine
		sem <- struct{}{}

		go func() {
			defer func() { <-sem }() // Release semaphore slot when done
			// 🛡️ Zero-Crash Hardening: Recover from any unexpected panics to keep the daemon running
			defer func() {
				if r := recover(); r != nil {
					log.Printf("CRITICAL: Panic recovered in ChangeReplicationMode handler for %s: %v", connection.RemoteAddr(), r)
				}
			}()

			comm := connection
			defer comm.Close()

			log.Printf("Client connected to changeReplicationMode")

			// 🛡️ Sentinel: Enforce a read/write timeout to prevent slowloris DoS attacks
			comm.SetAbsoluteDeadline(time.Now().Add(10 * time.Second))

			remoteAddr := common.SanitizeLog(connection.RemoteAddr().String())

			// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp,
			// validates the token, and returns the timestamp.
			_, ts, err := comm.HandshakeServer(expectedAuthToken)
			if err != nil {
				log.Printf("AUDIT: Handshake failed from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// 🛡️ Sentinel: Add audit logging for successful authentication
			log.Printf("AUDIT: Successful authentication for changeReplicationMode from %s", remoteAddr)

			// Send a dummy replication mode back to complete the handshake
			if err := comm.SendReplicationMode(0); err != nil {
				log.Printf("AUDIT: Error sending handshake ACK to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// Decode the replication data directly from the connection
			// 🛡️ Sentinel: Limit the JSON payload size to prevent DoS via memory exhaustion
			replicationJson := common.ReplicationData{}
			decoder := json.NewDecoder(io.LimitReader(comm, 1024))
			if err := decoder.Decode(&replicationJson); err != nil {
				log.Printf("AUDIT: Failed to decode replication data from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				return
			}

			// Update the replication state
			newState := SetReplicationState(replicationJson.New, ts)
			newReplicationJson, _ := json.Marshal(newState)
			// 🛡️ Sentinel: Audit log the sensitive operation
			log.Printf("AUDIT: Replication mode changed to %d by %s", replicationJson.New, remoteAddr)
			log.Printf("ReplicationData new struct: %s", string(newReplicationJson))

			// Send ACK back to client to confirm receipt and prevent premature connection termination
			if _, err := comm.Write([]byte("OK")); err != nil {
				log.Printf("AUDIT: Failed to send ACK to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
			}

			// If this is the primary server, propagate the change to the other servers
			if 0 == serverId {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("CRITICAL: Panic recovered in propagation to node 1: %v", r)
						}
					}()
					ChangeReplicationModeClient(factory, newReplicationJson, 1)
				}()
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("CRITICAL: Panic recovered in propagation to node 2: %v", r)
						}
					}()
					ChangeReplicationModeClient(factory, newReplicationJson, 2)
				}()
			}
		}()
	}
}

// ChangeReplicationModeClient connects to another server in the cluster and sends the new replication mode.
// It is used by the primary server to propagate replication mode changes to the other servers.
func ChangeReplicationModeClient(factory *transport.ProtocolFactory, replicationJson []byte, serverId int) {
	daemons := factory.GetDaemons()
	comm, err := factory.Dial(daemons[serverId].ChangeReplication)
	if err != nil {
		log.Printf("Dial error for server %d (%s): %v", serverId, daemons[serverId].ChangeReplication, common.SanitizeLog(err.Error()))
		return
	}
	defer comm.Close()

	// ⚡ Bolt: Consolidate AuthToken and JSON payload into a single optimally-sized buffer
	// to avoid multiple `conn.Write` calls and `string` allocation overhead.
	// For now, we still need to perform the handshake.
	// This will need more refactoring if we want to truly consolidate the writes across protocols.
	authToken := factory.GetAuthToken()
	timestamp := time.Now().UnixNano()
	// Perform handshake
	if _, err := comm.HandshakeClient(authToken, timestamp, 0); err != nil {
		log.Printf("Handshake failed with peer %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}


	// ⚡ Bolt: Use sync.Pool to minimize allocations during cluster-wide broadcasts.
	payload := payloadPool.Get().([]byte)
	payload = payload[:0]
	payload = append(payload, replicationJson...)
	payload = append(payload, '\n')
	defer payloadPool.Put(payload[:cap(payload)])

	if _, err := comm.Write(payload); err != nil {
		log.Printf("Failed to send ReplicationData to %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}

	// Wait for ACK to prevent premature connection termination, especially over QUIC
	// ⚡ Bolt: Eliminate heap allocation by using a stack-allocated byte array for io.ReadFull.
	var ackBuf [2]byte // We expect "OK"
	if _, err := io.ReadFull(comm, ackBuf[:]); err != nil {
		log.Printf("Failed to read ACK from %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}

	log.Printf("ReplicationData sent to serverId: %d", serverId)
}
