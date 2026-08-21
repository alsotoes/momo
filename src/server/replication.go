// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// payloadPoolCapacity is the fixed capacity of the reusable byte slices
// handed out by payloadPool. Only buffers with this exact capacity are ever
// returned to the pool; growth beyond it is discarded (fix #667).
const payloadPoolCapacity = 1024

// payloadPoolc is the minimal contract payloadPool needs: fetch a buffer and
// hand one back. sync.Pool satisfies it; tests substitute an instrumented
// implementation to assert returned capacities.
type payloadPoolc interface {
	Get() interface{}
	Put(interface{})
}

// payloadPool provides reusable byte slices for replication broadcasts to reduce allocations.
var payloadPool payloadPoolc = &sync.Pool{
	New: func() interface{} {
		return make([]byte, payloadPoolCapacity)
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

// acquireConnectionSlot reserves a slot in the bounded concurrency semaphore.
// It returns false without blocking if ctx is canceled before a slot frees up,
// so a shutdown never leaves the accept loop stuck on a full semaphore
// (issue #659). The caller must close/abandon any already-accepted connection.
func acquireConnectionSlot(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// ChangeReplicationModeServer listens for connections on a dedicated port and updates the replication mode of the server.
//
// When a client connects, it sends a JSON object containing the new replication mode.
// This function updates the server's replication mode and, if the server is the primary (serverId 0),
// it propagates the change to the other servers in the cluster.
func ChangeReplicationModeServer(ctx context.Context, cfg common.Configuration, serverId int, timestamp int64) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in ChangeReplicationModeServer: %v: %w", r, syscall.EIO)
			log.Printf("CRITICAL: %v", err)
		}
	}()

	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		return fmt.Errorf("server ID %d is out of range [0, %d)", serverId, len(daemons))
	}
	factory := transport.NewProtocolFactory(cfg)
	server, err := factory.Listen(daemons[serverId].ChangeReplication)
	if err != nil {
		return fmt.Errorf("Error listening: %v: %w", err, syscall.EIO)
	}

	closeOnce := sync.Once{}
	closeServer := func() { closeOnce.Do(func() { server.Close() }) }
	defer closeServer()

	// Handle graceful shutdown via context
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in ChangeReplicationMode shutdown handler: %v", r)
			}
		}()
		<-ctx.Done()
		closeServer()
	}()

	log.Printf("Server changeReplicationMode started... at %s", daemons[serverId].ChangeReplication)
	log.Printf("Waiting for connections: changeReplicationMode...")
	log.Printf("default ReplicationMode value: %d", GetCurrentReplicationMode())

	// Initialize the replication state
	initialState := SetReplicationState(GetCurrentReplicationMode(), timestamp)
	replicationJson, err := json.Marshal(initialState)
	if err != nil {
		log.Printf("AUDIT: Failed to marshal initial replication state: %v", fmt.Errorf("%v: %w", common.SanitizeLog(err.Error()), syscall.EIO))
	}
	log.Printf("ReplicationData struct: %s", string(replicationJson))

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))

	// 🛡️ Sentinel: Adaptive failed-auth backoff & temporary lockout (issue #821).
	authLimiter := common.NewAuthLimiter(time.Duration(cfg.Global.AuthBackoffDelay) * time.Millisecond)

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

		// Acquire semaphore slot before spinning up a new goroutine. Blocking
		// without a ctx.Done() branch would leak this goroutine if the
		// semaphore were full and the context canceled (issue #659).
		if !acquireConnectionSlot(ctx, sem) {
			connection.Close()
			return nil // Shutting down gracefully
		}

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

			// 🛡️ Sentinel: Adaptive failed-auth backoff & temporary lockout (issue #821).
			if authLimiter.Enabled() && !authLimiter.Allow(remoteAddr) {
				log.Printf("AUTH: rejected connection from rate-limited source %s", remoteAddr)
				return
			}

			// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp,
			// validates the token, and returns the timestamp.
			_, ts, err := comm.HandshakeServer(expectedAuthToken)
			if err != nil {
				log.Printf("AUDIT: Handshake failed from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				if authLimiter.Enabled() {
					authLimiter.RecordFailure(remoteAddr)
				}
				return
			}

			// 🛡️ Sentinel: Add audit logging for successful authentication
			log.Printf("AUDIT: Successful authentication for changeReplicationMode from %s", remoteAddr)
			if authLimiter.Enabled() {
				authLimiter.RecordSuccess(remoteAddr)
			}

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
			newReplicationJson, marshalErr := json.Marshal(newState)
			if marshalErr != nil {
				log.Printf("AUDIT: Failed to marshal new replication state: %v", fmt.Errorf("%v: %w", common.SanitizeLog(marshalErr.Error()), syscall.EIO))
			}
			// 🛡️ Sentinel: Audit log the sensitive operation
			log.Printf("AUDIT: Replication mode changed to %d by %s", replicationJson.New, remoteAddr)
			log.Printf("ReplicationData new struct: %s", string(newReplicationJson))

			// Send ACK back to client to confirm receipt and prevent premature connection termination
			if _, err := comm.Write([]byte("OK")); err != nil {
				log.Printf("AUDIT: Failed to send ACK to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
			}

			// If this is the primary server, propagate the change to the other servers.
			// Skip propagation if json.Marshal failed to avoid sending empty data to peers.
			if 0 == serverId && marshalErr == nil {
				daemons := factory.GetDaemons()
				var propWg sync.WaitGroup
				for i := range daemons {
					if i == serverId {
						continue
					}
					propWg.Add(1)
					go func(id int) {
						defer propWg.Done()
						defer func() {
							if r := recover(); r != nil {
								err := fmt.Errorf("panic in propagation to node %d: %v: %w", id, r, syscall.EIO)
								log.Printf("CRITICAL: %v", err)
							}
						}()
						ChangeReplicationModeClient(factory, newReplicationJson, id)
					}(i)
				}
				// Timeout-bounded wait: each peer has a 10s deadline in ChangeReplicationModeClient.
				// Bound the wait to 11s to avoid blocking the control plane indefinitely on unresponsive peers.
				propDone := make(chan struct{})
				go func() {
					propWg.Wait()
					close(propDone)
				}()
				select {
				case <-propDone:
				case <-time.After(11 * time.Second):
					log.Printf("AUDIT: Propagation timed out after 11s, some peers may not have received replication update from %s", remoteAddr)
				}
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
	// 🛡️ CVE-007: Use the derived peer token for peer-to-peer connections so the
	// receiving server can cryptographically distinguish peers from clients.
	peerToken := common.DerivePeerTokenString(authToken)
	timestamp := time.Now().UnixNano()
	// Perform handshake
	if _, err := comm.HandshakeClient(peerToken, timestamp, 0); err != nil {
		log.Printf("Handshake failed with peer %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}

	// ⚡ Bolt: Use sync.Pool to minimize allocations during cluster-wide broadcasts.
	payload := payloadPool.Get().([]byte)
	payload = payload[:0]
	payload = append(payload, replicationJson...)
	payload = append(payload, '\n')
	// 🛡️ Sentinel: Only return the buffer to the pool if append did not
	// reallocate it. When the payload exceeds payloadPoolCapacity, a new,
	// larger slice is allocated and the fixed-capacity pool buffer is lost.
	// Returning the grown slice thrashes the pool and defeats its purpose,
	// so it is released instead (fix #667).
	defer releasePayload(payload)

	if _, err := comm.Write(payload); err != nil {
		log.Printf("Failed to send ReplicationData to %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}

	// Wait for ACK to prevent premature connection termination, especially over QUIC.
	// Enforce a strict 10s timeout to prevent goroutine leaks from unresponsive peers.
	comm.SetAbsoluteDeadline(time.Now().Add(10 * time.Second))
	var ackBuf [2]byte // We expect "OK"
	if _, err := io.ReadFull(comm, ackBuf[:]); err != nil {
		log.Printf("Failed to read ACK from %d: %v", serverId, common.SanitizeLog(err.Error()))
		return
	}

	log.Printf("ReplicationData sent to serverId: %d", serverId)
}

// releasePayload returns a replication payload buffer to the pool, but only if
// it still has the fixed pool capacity. A payload that outgrew its original
// buffer (append reallocated) never came from the pool as the grown slice, so
// the pooled fixed-capacity buffer has already been lost; returning the grown
// slice would poison the pool with oversized buffers (fix #667).
func releasePayload(payload []byte) {
	if cap(payload) == payloadPoolCapacity {
		payloadPool.Put(payload[:payloadPoolCapacity])
	}
}
