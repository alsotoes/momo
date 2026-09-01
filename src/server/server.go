// Package server provides the core functionality for the momo server.
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/client"
	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// connectToPeer is an alias for the client.Connect function, used to connect to other servers in the cluster for data replication.
var connectToPeer = client.Connect

// forwardStreamFunc is the signature of client.ConnectStream, used for streaming
// replication forwarding from a store reader instead of a local file path.
// It is a named type so tests may swap it atomically without racing concurrent
// connection-handler goroutines (issue #628).
type forwardStreamFunc func(cfg common.Configuration, content io.Reader, name, hash string, size int64, remotePath string, s3Headers map[string]string, serverId int, timestamp int64, requestedMode int, replicationFactor int) error

// connectToPeerStream is an atomic pointer to client.ConnectStream. It is atomic
// so tests may override the forwarder without a data race against the daemon's
// concurrent forwarding goroutines.
var connectToPeerStream atomic.Pointer[forwardStreamFunc]

// replicationForwardTimeout bounds how long the server waits for replication
// forwarding goroutines to complete before abandoning them. Without this bound,
// a hung peer or stalled backing store would block the connection-handler
// goroutine indefinitely (issue #628). It is atomic so tests may shorten the
// window without a data race against concurrent connection handlers.
var replicationForwardTimeout atomic.Int64

func init() {
	replicationForwardTimeout.Store(int64(30 * time.Second))
	fwd := forwardStreamFunc(client.ConnectStream)
	connectToPeerStream.Store(&fwd)
}

// forwardTimeout returns the current replication forwarding wait deadline.
func forwardTimeout() time.Duration {
	return time.Duration(replicationForwardTimeout.Load())
}

// forwardStream returns the current connectToPeerStream implementation.
func forwardStream() forwardStreamFunc {
	return *connectToPeerStream.Load()
}

// Daemon is the core of the momo server.
// It listens for incoming connections and handles file uploads and replication.
// The server's behavior is determined by the replicationMode, which is received from the client.
//
// The server can operate in one of the following replication modes:
//   - ReplicationNone: The server saves the file without replicating it to other nodes.
//   - ReplicationSplay: The primary server replicates the file to all other servers in the cluster.
//   - ReplicationChain: Servers are arranged in a chain. The primary server replicates to the next server in the chain, which then replicates to the next, and so on.
//   - ReplicationPrimarySplay: Client-side replication. The momo client fans the
//     upload out to replication_factor nodes (CRUSH placement) concurrently
//     (client.go:226); the receiving server just stores locally and must NOT
//     forward, so this mode shares the ReplicationNone store path. For external
//     S3 clients, which cannot perform client-side fan-out, the server downgrades
//     the mode to the next server-side mode in replication_order
//     (client_side_replication_modes) per connection.
//
// The replication mode is determined by the client, and for secondary servers, it's influenced by the timestamp of the operation.
func Daemon(ctx context.Context, cfg common.Configuration, serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in Daemon: %v", r)
			err = syscall.EIO
		}
	}()

	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		return fmt.Errorf("server id out of range")
	}
	factory := transport.NewProtocolFactory(cfg)

	// Initialize storage with configured backend.
	// When E2EE is enabled, derive the server-side at-rest (SSE) key from
	// the tenant key scoped to the at-rest domain, so it is distinct from
	// the transport content key and never the raw master key.
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
	store, err := storage.NewStore(cfg.Storage, daemons[serverId], encKeyHex)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer store.Close()

	server, err := factory.Listen(daemons[serverId].Host)
	if err != nil {
		return fmt.Errorf("Error listening: %v", err)
	}

	closeOnce := sync.Once{}
	closeServer := func() { closeOnce.Do(func() { server.Close() }) }
	defer closeServer()

	var handlersWG sync.WaitGroup
	defer handlersWG.Wait()

	// Handle graceful shutdown via context
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in Daemon shutdown handler: %v", r)
			}
		}()
		<-ctx.Done()
		closeServer()
	}()

	log.Printf("Server primary Daemon started... at %s using %s", daemons[serverId].Host, cfg.Global.Protocol)
	log.Printf("...Waiting for connections...")

	// Start Prometheus metrics endpoint if configured. Per-daemon override
	// (metrics_host / metrics_port) falls back to the global [metrics] bind.
	metricsCollector := NewMetricsCollector()
	metricsHost := daemons[serverId].MetricsBindHost
	if metricsHost == "" {
		metricsHost = cfg.Metrics.PrometheusBindHost
	}
	metricsPort := daemons[serverId].MetricsBindPort
	if metricsPort == 0 {
		metricsPort = cfg.Metrics.PrometheusPort
	}
	// R5 phase 2: install scrape-time storage/CAS gauge source (blob count,
	// stored bytes, disk, GC) when the store implements it.
	if ssp, ok := store.(storageStatsProvider); ok {
		metricsCollector.SetStorageStats(ssp)
	}
	// R5 phase 4: opt-in latency histograms (zero overhead when disabled).
	metricsCollector.SetLatencyHistogramsEnabled(cfg.Metrics.EnableLatencyHistograms)
	StartMetricsServer(ctx, metricsHost, metricsPort, metricsCollector)

	// 🛡️ Sentinel: Adaptive failed-auth backoff & temporary lockout (issue #821).
	// Disabled (Allow always true) when auth_backoff_delay is 0.
	authLimiter := common.NewAuthLimiter(time.Duration(cfg.Global.AuthBackoffDelay) * time.Millisecond)

	// 🛡️ Zero-Crash: Log a warning if the cluster cannot meet the desired durability goal.
	if cfg.Global.ReplicationFactor > len(daemons) {
		log.Printf("⚠️ WARNING: Desired replication factor (%d) exceeds available node count (%d). Data will be stored in DEGRADED mode.", cfg.Global.ReplicationFactor, len(daemons))
	}

	// ⚡ Bolt: Hoist constant AuthToken padding and conversion out of the loop.
	expectedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))

	// ⚡ Bolt: Pre-build the ClusterMap during boot to avoid per-request allocations.
	nodes := make([]*common.Node, len(cfg.Daemons))
	for i, d := range cfg.Daemons {
		nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host, Domain: d.FailureDomain}
	}
	cmap := &common.ClusterMap{Nodes: nodes}

	// P2P Transport & Gossip (coexists with existing listener when enabled)
	var scatterGather *p2p.ScatterGather
	var leaseManager *p2p.LeaseManager
	var oprfProvider *p2p.OPRFProvider
	if cfg.P2P.Enabled {
		scatterGather, leaseManager, oprfProvider = bootstrapP2P(ctx, cfg, serverId, daemons, store)
	}
	// R5 phase 3: install scrape-time P2P cluster gauge source (nil-safe when
	// P2P is disabled — exporter degrades to zero gauges per spec GIVEN p2p).
	if scatterGather != nil {
		metricsCollector.SetClusterStats(newP2PClusterStats(scatterGather, leaseManager))
	}

	// Client-facing threshold-OPRF service for confidential dedup.
	var oprfService transport.OPRFService
	if cfg.Global.OPRFEnabled {
		var sErr error
		oprfService, sErr = newDaemonOPRFService(daemons[serverId], oprfProvider, cfg.Global.OPRFThreshold)
		if sErr != nil {
			log.Printf("Failed to initialize OPRF service: %v", sErr)
			return
		}
	}

	// 🛡️ Sentinel: Enforce a limit on concurrent connections to prevent resource exhaustion (DoS).
	const maxConcurrentConnections = 1000
	sem := make(chan struct{}, maxConcurrentConnections)

	// Accept loop: server.Accept() returns errors, not panics. Per-connection
	// goroutines below each have their own recover() block (Rule 37) for panic safety.
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

		// Acquire semaphore slot before spinning up a new goroutine; if the slot
		// cannot be acquired because the server is shutting down, close the
		// accepted connection before returning (resource-leak prevention).
		if !acquireConnectionSlot(ctx, sem) {
			connection.Close()
			return syscall.ECANCELED
		}
		handlersWG.Add(1)
		go func(comm transport.Communicator) {
			defer handlersWG.Done()
			defer func() { <-sem }()
			// 🛡️ Zero-Crash Hardening: Recover from any unexpected panics in the connection handler
			// to ensure the daemon remains stable and available for other clients.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("CRITICAL: Panic recovered in Daemon for %s: %v", comm.RemoteAddr(), r)
					comm.Close()
					metricsCollector.IncErrors()
				}
			}()

			metricsCollector.IncConnections()
			defer metricsCollector.DecConnections()

			var replicationMode int
			var success bool

			// R5 phase 4: capture upload-start time only when latency histograms
			// are armed (zero cost otherwise).
			var uploadStart time.Time
			if metricsCollector.HistogramsEnabled() {
				uploadStart = time.Now()
			}

			// 🛡️ Sentinel: Capture remote address for audit logging and traceability
			remoteAddr := common.SanitizeLog(comm.RemoteAddr().String())

			// 🛡️ Sentinel: Adaptive failed-auth backoff & temporary lockout (issue #821).
			// When enabled, reject a source that is under backoff/lockout BEFORE any
			// crypto/network work to meter online brute-force attempts.
			if authLimiter.Enabled() && !authLimiter.Allow(remoteAddr) {
				log.Printf("AUTH: rejected connection from rate-limited source %s", remoteAddr)
				defer func() {
					metricsCollector.IncErrors()
				}()
				return
			}

			// Inject storage store if the communicator supports it (e.g. S3 for list/delete)
			if s3Comm, ok := comm.(interface{ SetStore(storage.Store) }); ok {
				s3Comm.SetStore(store)
			}

			// Inject the configured S3 bucket name for bucket-management operations
			if bcComm, ok := comm.(interface{ SetConfiguredBucket(string) }); ok {
				bcComm.SetConfiguredBucket(cfg.Storage.S3Bucket)
			}

			// Inject dedicated S3 gateway SigV4 credentials (issue #656). When
			// configured, inbound SigV4 requests must present these access/secret
			// keys instead of the momo auth token.
			if scComm, ok := comm.(interface{ SetSigV4GatewayCredentials(string, string) }); ok {
				scComm.SetSigV4GatewayCredentials(cfg.Storage.S3ServerAccessKey, cfg.Storage.S3ServerSecretKey)
			}

			// Inject metrics hook for download/delete/error instrumentation
			if mhComm, ok := comm.(interface{ SetMetricsHook(transport.MetricsHook) }); ok {
				mhComm.SetMetricsHook(metricsCollector)
			}

			// Inject scatter-gather and lease capabilities if P2P is enabled
			if scatterGather != nil {
				if glComm, ok := comm.(interface{ SetGlobalLister(transport.GlobalLister) }); ok {
					glComm.SetGlobalLister(NewScatterGatherLister(scatterGather, store,
						time.Duration(cfg.P2P.ScatterGatherTimeout)*time.Second))
				}
				if dpComm, ok := comm.(interface {
					SetDeletePropagator(transport.DeletePropagator)
				}); ok {
					dpComm.SetDeletePropagator(NewScatterGatherDeleter(scatterGather,
						time.Duration(cfg.P2P.ScatterGatherTimeout)*time.Second))
				}
			}
			if leaseManager != nil {
				if laComm, ok := comm.(interface{ SetLeaseAcquirer(transport.LeaseAcquirer) }); ok {
					laComm.SetLeaseAcquirer(NewLeaseAcquirerAdapter(leaseManager))
				}
			}
			if oprfService != nil {
				if opComm, ok := comm.(interface{ SetOPRFService(transport.OPRFService) }); ok {
					opComm.SetOPRFService(oprfService)
				}
			}

			// 🛡️ Sentinel: Apply a strict absolute deadline for the handshake phase to prevent Slowloris trickle attacks.
			comm.SetAbsoluteDeadline(time.Now().Add(10 * time.Second))

			defer func() {
				if success {
					log.Printf("AUDIT: Server ACK to Client %s => ACK%d", remoteAddr, serverId)
					if err := comm.SendACK(serverId); err != nil {
						log.Printf("ERROR: Failed to send ACK to client %s: %v", remoteAddr, err)
					}
				}
				comm.Close()
			}()

			var ts int64
			var err error
			// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp + RequestedMode,
			// validates the token, and returns the timestamp and requested mode.
			replicationMode, ts, err = comm.HandshakeServer(expectedAuthToken)
			if err != nil {
				if err == transport.ErrRequestHandled {
					// The request was completely handled by the gateway layer (e.g., list, get, delete)
					success = false
					return
				}
				log.Printf("AUDIT: Handshake failed from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				metricsCollector.IncErrors()
				// 🛡️ Sentinel: record the failure for adaptive backoff (issue #821).
				if authLimiter.Enabled() {
					authLimiter.RecordFailure(remoteAddr)
				}
				return
			}

			// 🛡️ Sentinel: Add audit logging for successful authentication
			log.Printf("AUDIT: Successful authentication from %s", remoteAddr)
			// 🛡️ Sentinel: successful auth releases any backoff/lockout (issue #821).
			if authLimiter.Enabled() {
				authLimiter.RecordSuccess(remoteAddr)
			}

			// Determine the replication mode based on whether we are the Primary or a Secondary.
			// 🛡️ CVE-007: Use cryptographic peer authentication (IsPeer) instead of the
			// insecure DummyEpoch timestamp check. An attacker who knows the auth token
			// can no longer impersonate a peer by sending a fake timestamp.
			repState := GetReplicationState()
			var finalTs int64

			if !comm.IsPeer() {
				// We are the Primary for this transaction (direct client connection).
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

			// Downgrade client-side replication modes for external S3 clients.
			// External clients (e.g., aws-cli) cannot perform client-side replication
			// (primary-splay). If the selected mode is in ClientSideReplicationModes,
			// walk ReplicationOrder forward to find the next server-side mode.
			// This is a per-transaction downgrade — global polymorphic state is unchanged.
			if comm.IsExternalClient() {
				for _, csm := range cfg.Global.ClientSideReplicationModes {
					if replicationMode == csm {
						originalMode := replicationMode
						replicationMode = downgradeToServerSideMode(replicationMode, cfg.Global.ReplicationOrder, cfg.Global.ClientSideReplicationModes)
						log.Printf("AUDIT: External client detected — downgraded replication mode %d → %d (per-transaction, global state unchanged)", originalMode, replicationMode)
						break
					}
				}
			}

			log.Printf("Cluster object global timestamp: %d", finalTs)
			log.Printf("Server Daemon replicationMode: %d", replicationMode)

			// Send the selected replication mode back to the client
			if err := comm.SendReplicationMode(replicationMode); err != nil {
				log.Printf("AUDIT: Error sending replication mode to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				metricsCollector.IncErrors()
				return
			}

			// 🛡️ Sentinel: Extend the absolute deadline to allow the client time to establish
			// splay connections and pre-compute file hashes before sending metadata.
			comm.SetAbsoluteDeadline(time.Now().Add(60 * time.Second))

			metadata, err := comm.ReceiveMetadata()
			if err != nil {
				log.Printf("AUDIT: Error getting metadata from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
				metricsCollector.IncErrors()
				comm.Close()
				return
			}

			// 🛡️ Sentinel: Sanitize Hash immediately to prevent path traversal in all downstream consumers.
			if metadata.Hash == "" || strings.Contains(metadata.Hash, ".") || strings.Contains(metadata.Hash, "/") || strings.Contains(metadata.Hash, "\\") {
				log.Printf("AUDIT: Invalid hash received from %s: %v", remoteAddr, common.SanitizeLog(metadata.Hash))
				// ⚡ Bolt: Map to syscall.EBADMSG for POSIX compliance.
				success = false
				metricsCollector.IncErrors()
				return
			}

			// 🛡️ Sentinel: Sanitize and normalize fileName to prevent path traversal attacks (Rule 4).
			rawFileName := metadata.Name
			if rawFileName == "" || rawFileName == "." || rawFileName == ".." || strings.Contains(rawFileName, "../") || strings.Contains(rawFileName, "\\") {
				log.Printf("AUDIT: Invalid filename received from %s: %v", remoteAddr, common.SanitizeLog(rawFileName))
				success = false
				metricsCollector.IncErrors()
				return
			}
			remotePath := ""
			fileName := filepath.Base(rawFileName)
			if strings.Contains(rawFileName, "/") {
				remotePath = filepath.Dir(rawFileName)
			}
			if fileName == "" || fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
				log.Printf("AUDIT: Invalid filename received from %s: %v", remoteAddr, common.SanitizeLog(fileName))
				success = false
				metricsCollector.IncErrors()
				return
			}
			storageKey := rawFileName

			// 🛡️ Sentinel: Enforce maximum file size to prevent Denial of Service via resource exhaustion
			if metadata.Size < 0 || metadata.Size > common.MaxFileSize {
				log.Printf("AUDIT: Invalid file size received from %s: %d (max: %d)", remoteAddr, metadata.Size, common.MaxFileSize)
				// ⚡ Bolt: Map to syscall.EBADMSG for POSIX compliance.
				success = false
				metricsCollector.IncErrors()
				return
			}

			// 🛡️ Zero-Crash: Defensive check for storage initialization.
			if store == nil {
				log.Printf("AUDIT: Storage error for %s: store not initialized: %v", remoteAddr, syscall.EIO)
				metricsCollector.IncErrors()
				return
			}

			// ⚡ Bolt: Content-Addressable Deduplication Check.
			exists, err := store.Has(metadata.Hash)
			if err != nil {
				log.Printf("AUDIT: Storage error checking hash %s: %v", metadata.Hash, common.SanitizeLog(err.Error()))
				metricsCollector.IncErrors()
				return
			}

			// 🛡️ CVE-005: Prevent deduplication confusion attack.
			// Only skip payload if the name already maps to the same hash (legitimate re-upload).
			// If the hash exists but the name is new or maps to a different hash, require the
			// payload as proof of content knowledge before creating a new namespace alias.
			canDedup := false
			if exists {
				existingHash, hashErr := store.GetHashForName(storageKey)
				if hashErr == nil && existingHash == metadata.Hash {
					canDedup = true
				}
			}

			if canDedup {
				// ⚡ Bolt: Deduplication hit. Just update metadata mapping without reading payload.
				log.Printf("AUDIT: Deduplication hit for %s (hash: %s)", remoteAddr, metadata.Hash)
				metricsCollector.IncDedupHits()
				if err := comm.SendMetadataStatus(transport.MetadataStatusSkipPayload); err != nil {
					log.Printf("AUDIT: Error sending metadata status to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
					metricsCollector.IncErrors()
					return
				}
			} else {
				if err := comm.SendMetadataStatus(transport.MetadataStatusSendPayload); err != nil {
					log.Printf("AUDIT: Error sending metadata status to %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
					metricsCollector.IncErrors()
					return
				}
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
				metricsCollector.IncErrors()
				return
			}

			// 🛡️ Sentinel: Apply an absolute deadline based on file size.
			absoluteDeadline := time.Now().Add(5*time.Minute + time.Duration(metadata.Size/(10*1024*1024))*time.Minute)
			comm.SetAbsoluteDeadline(absoluteDeadline)

			var wg sync.WaitGroup

			// forwardFailed tracks whether any replication forwarding goroutine
			// failed or timed out, so the success ACK is suppressed and no silent
			// data loss goes undetected (Rule 9 / issue #627).
			var forwardMu sync.Mutex
			forwardFailed := false
			markForwardFail := func() {
				forwardMu.Lock()
				forwardFailed = true
				forwardMu.Unlock()
			}

			// waitForForwarders blocks until all replication forwarding goroutines
			// complete, bounded by a hard deadline. If a forwarder hangs (e.g. an
			// unresponsive peer or stalled S3 backing store), the forwarding is
			// abandoned so the connection-handler goroutine is never leaked
			// forever (Rule 4 / issue #628).
			waitForForwarders := func() {
				done := make(chan struct{})
				go func() {
					defer close(done)
					wg.Wait()
				}()
				select {
				case <-done:
					// all forwarders completed
				case <-time.After(forwardTimeout()):
					log.Printf("AUDIT: Replication forwarding timed out after %v for %s", forwardTimeout(), fileName)
					metricsCollector.IncErrors()
					markForwardFail()
				}
			}

			// Handle the file based on the replication mode
			switch replicationMode {
			// ReplicationPrimarySplay is intentionally grouped with ReplicationNone:
			// replication is driven by the momo client (client.go:226), so the
			// receiving server stores locally without forwarding. External S3
			// clients are downgraded out of this mode earlier (issue #658).
			case common.ReplicationNone, common.ReplicationPrimarySplay:
				if canDedup {
					// ⚡ Bolt: Deduplication hit. Just update metadata mapping without reading payload.
					if err := store.Put(storageKey, metadata.Hash, metadata.Size, remotePath, nil); err != nil {
						log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
						metricsCollector.IncErrors()
						return
					}
					persistS3Meta(store, storageKey, metadata)
				} else {
					if err := getFile(comm, store, storageKey, metadata.Hash, metadata.Size, remotePath); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
						metricsCollector.IncErrors()
						return
					}
					persistS3Meta(store, storageKey, metadata)
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
				if canDedup {
					// ⚡ Bolt: Deduplication hit. Just update metadata mapping without reading payload.
					if err := store.Put(storageKey, metadata.Hash, metadata.Size, remotePath, nil); err != nil {
						log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
						wg.Done()
						metricsCollector.IncErrors()
						return
					}
					persistS3Meta(store, storageKey, metadata)
				} else {
					if err := getFile(comm, store, storageKey, metadata.Hash, metadata.Size, remotePath); err != nil {
						log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
						wg.Done()
						metricsCollector.IncErrors()
						return
					}
					persistS3Meta(store, storageKey, metadata)
				}

				if myPos != -1 && myPos < len(placement)-1 {
					nextHop := placement[myPos+1]
					log.Printf("AUDIT: Chain forwarding from Node %d to Node %d", serverId, nextHop.ID)

					// 🛡️ Zero-Crash: Wrap Chain forwarding in a goroutine with recovery for consistency and safety.
					go func(id int) {
						defer wg.Done()
						defer func() {
							if r := recover(); r != nil {
								log.Printf("CRITICAL: Panic recovered in Chain forwarder to node %d: %v", id, r)
								metricsCollector.IncErrors()
							}
						}()
						reader, _, err := store.Get(storageKey)
						if err != nil {
							log.Printf("AUDIT: Failed to get blob for chain forwarding: %v", common.SanitizeLog(err.Error()))
							metricsCollector.IncErrors()
							markForwardFail()
							return
						}
						defer reader.Close()
						// R5 phase 4: time replication only when histograms are armed.
						var replStart time.Time
						if metricsCollector.HistogramsEnabled() {
							replStart = time.Now()
						}
						fwdErr := forwardStream()(cfg, reader, storageKey, metadata.Hash, metadata.Size, "", getS3Meta(store, storageKey), id, finalTs, replicationMode, factor)
						if fwdErr != nil {
							log.Printf("AUDIT: Chain forwarding to node %d failed: %v", id, common.SanitizeLog(fwdErr.Error()))
							metricsCollector.IncErrors()
							metricsCollector.IncReplicationFailures()
							markForwardFail()
							return
						}
						metricsCollector.AddBytesReplicated(uint64(metadata.Size))
						if !replStart.IsZero() {
							metricsCollector.RecordReplicationLatency(time.Since(replStart))
						}
					}(nextHop.ID)
				} else {
					wg.Done()
				}
				waitForForwarders()

			case common.ReplicationSplay:
				// In Splay mode, the primary (first node in placement) forwards to all others.
				// An external client (e.g. aws-cli) connects directly without client-side
				// placement, so it may hit any node. 🛡️ If that node is a placement member,
				// have it drive replication too — otherwise the upload would be stored on a
				// single node and never replicated (issue #647). Guard against an empty
				// placement list (e.g. ReplicationFactor 0 or no nodes) to avoid an
				// index-out-of-range panic — fall through to local receive.
				if len(placement) > 0 && (placement[0].ID == serverId || comm.IsExternalClient()) {
					// Forward to every placement member except this node. When self is the
					// primary this is placement[1:]; an external client that landed on a
					// non-primary member still replicates to the rest of the set (issue #647).
					targets := make([]int, 0, len(placement)-1)
					for _, n := range placement {
						if n.ID != serverId {
							targets = append(targets, n.ID)
						}
					}
					wg.Add(len(targets))
					if canDedup {
						// ⚡ Bolt: Deduplication hit. Just update metadata mapping.
						if err := store.Put(storageKey, metadata.Hash, metadata.Size, remotePath, nil); err != nil {
							log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
							for range targets {
								wg.Done()
							}
							metricsCollector.IncErrors()
							return
						}
						persistS3Meta(store, storageKey, metadata)
					} else {
						if err := getFile(comm, store, storageKey, metadata.Hash, metadata.Size, remotePath); err != nil {
							log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
							for range targets {
								wg.Done()
							}
							metricsCollector.IncErrors()
							return
						}
						persistS3Meta(store, storageKey, metadata)
					}
					for _, targetId := range targets {
						go func(id int) {
							defer wg.Done()
							defer func() {
								if r := recover(); r != nil {
									log.Printf("CRITICAL: Panic recovered in Splay forwarder to node %d: %v", id, r)
									metricsCollector.IncErrors()
								}
							}()
							reader, _, err := store.Get(storageKey)
							if err != nil {
								log.Printf("AUDIT: Failed to get blob for splay forwarding: %v", common.SanitizeLog(err.Error()))
								metricsCollector.IncErrors()
								markForwardFail()
								return
							}
							defer reader.Close()
							// R5 phase 4: time replication only when histograms are armed.
							var replStart time.Time
							if metricsCollector.HistogramsEnabled() {
								replStart = time.Now()
							}
							fwdErr := forwardStream()(cfg, reader, storageKey, metadata.Hash, metadata.Size, "", getS3Meta(store, storageKey), id, finalTs, replicationMode, factor)
							if fwdErr != nil {
								log.Printf("AUDIT: Splay forwarding to node %d failed: %v", id, common.SanitizeLog(fwdErr.Error()))
								metricsCollector.IncErrors()
								metricsCollector.IncReplicationFailures()
								markForwardFail()
								return
							}
							metricsCollector.AddBytesReplicated(uint64(metadata.Size))
							if !replStart.IsZero() {
								metricsCollector.RecordReplicationLatency(time.Since(replStart))
							}
						}(targetId)
					}
					waitForForwarders()
				} else {
					// We are a secondary in a splay, just receive the file if needed.
					if canDedup {
						if err := store.Put(storageKey, metadata.Hash, metadata.Size, remotePath, nil); err != nil {
							log.Printf("AUDIT: Error updating metadata for %s from %s: %v", fileName, remoteAddr, common.SanitizeLog(err.Error()))
							metricsCollector.IncErrors()
							return
						}
						persistS3Meta(store, storageKey, metadata)
					} else {
						if err := getFile(comm, store, storageKey, metadata.Hash, metadata.Size, remotePath); err != nil {
							log.Printf("AUDIT: Error getting file from %s: %v", remoteAddr, common.SanitizeLog(err.Error()))
							metricsCollector.IncErrors()
							return
						}
						persistS3Meta(store, storageKey, metadata)
					}
				}
			default:
				log.Printf("AUDIT: *** ERROR: Unknown replication type from %s", remoteAddr)
				metricsCollector.IncErrors()
				return
			}
			forwardMu.Lock()
			forwardFailedFlag := forwardFailed
			forwardMu.Unlock()
			if forwardFailedFlag {
				log.Printf("AUDIT: Upload from %s stored locally but replication forwarding failed", remoteAddr)
				metricsCollector.IncErrors()
				return
			}
			success = true
			metricsCollector.IncUploads()
			if !canDedup {
				metricsCollector.AddBytesUploaded(uint64(metadata.Size))
			}
			if !uploadStart.IsZero() {
				metricsCollector.RecordRequestLatency("upload", time.Since(uploadStart))
			}
		}(connection)
	}
}

// persistS3Meta stores preserved S3 object metadata (Content-Type, x-amz-meta-*)
// at rest keyed by the storage key (issue #772). Uses an optional type assertion
// so stores that do not implement PutS3Meta simply skip persistence.
func persistS3Meta(store storage.Store, key string, meta common.FileMetadata) {
	if len(meta.S3Headers) == 0 {
		return
	}
	ps, ok := store.(interface {
		PutS3Meta(string, map[string]string) error
	})
	if !ok {
		return
	}
	if err := ps.PutS3Meta(key, meta.S3Headers); err != nil {
		log.Printf("AUDIT: Error persisting S3 metadata for %s: %v", key, common.SanitizeLog(err.Error()))
	}
}

// getS3Meta reads preserved S3 object metadata at rest for replication
// forwarding. Uses an optional type assertion; returns nil when unsupported.
func getS3Meta(store storage.Store, key string) map[string]string {
	gs, ok := store.(interface {
		GetS3Meta(string) map[string]string
	})
	if !ok {
		return nil
	}
	return gs.GetS3Meta(key)
}

// buildP2PTLSConfig constructs a *tls.Config for the P2P transport from the
// configuration. Returns nil if TLS is not configured (caller should log a
// warning). The config uses mutual TLS authentication: the node presents its
// own certificate and verifies peers against the CA.
func buildP2PTLSConfig(cfg common.Configuration) *tls.Config {
	if cfg.P2P.TLSCertFile == "" || cfg.P2P.TLSKeyFile == "" {
		return nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.P2P.TLSCertFile, cfg.P2P.TLSKeyFile)
	if err != nil {
		log.Printf("P2P: failed to load TLS cert/key (%s, %s): %v — falling back to plaintext", cfg.P2P.TLSCertFile, cfg.P2P.TLSKeyFile, err)
		return nil
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.P2P.TLSCAFile != "" {
		caData, err := os.ReadFile(cfg.P2P.TLSCAFile)
		if err != nil {
			log.Printf("P2P: failed to read CA cert file %s: %v — peer verification disabled", cfg.P2P.TLSCAFile, err)
		} else {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caData) {
				tlsConfig.RootCAs = pool
				tlsConfig.ClientCAs = pool
				tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			} else {
				log.Printf("P2P: failed to parse CA cert from %s — peer verification disabled", cfg.P2P.TLSCAFile)
			}
		}
	}

	return tlsConfig
}

// bootstrapP2P starts the P2P transport and gossip protocol alongside the main daemon.
// It connects to all configured daemon peers as bootstrap seeds and begins
// exchanging heartbeats for dynamic membership discovery.
// Returns the ScatterGather and LeaseManager instances for use by the server.
func bootstrapP2P(ctx context.Context, cfg common.Configuration, serverId int, daemons []*common.Daemon, store storage.Store) (*p2p.ScatterGather, *p2p.LeaseManager, *p2p.OPRFProvider) {
	gossipAddr := daemons[serverId].Host
	host, _, err := net.SplitHostPort(gossipAddr)
	if err != nil {
		host = "0.0.0.0"
	}

	basePort, err := strconv.Atoi(cfg.P2P.GossipPort)
	if err != nil {
		basePort = 4450
	}
	gossipPort := basePort + serverId
	gossipAddr = net.JoinHostPort(host, strconv.Itoa(gossipPort))

	tlsConfig := buildP2PTLSConfig(cfg)
	if tlsConfig == nil {
		log.Printf("CRITICAL: P2P transport is running without TLS — all P2P traffic is plaintext. Configure tls_cert_file, tls_key_file, and tls_ca_file under [p2p] for production.")
	}

	transport := p2p.NewTCPTransport(p2p.TCPTransportConfig{
		LocalID:   int32(serverId),
		AuthFunc:  func(id int32) bool { return id >= 0 && int(id) < len(daemons) },
		TLSConfig: tlsConfig,
	})

	if err := transport.Listen(gossipAddr); err != nil {
		log.Printf("P2P: failed to listen on %s: %v", gossipAddr, err)
		return nil, nil, nil
	}

	gossipCfg := p2p.GossipConfig{
		LocalID:           int32(serverId),
		HeartbeatInterval: time.Duration(cfg.P2P.GossipInterval) * time.Second,
		SuspicionTimeout:  time.Duration(cfg.P2P.SuspicionTimeout) * time.Second,
		Fanout:            cfg.P2P.Fanout,
		PingTimeout:       time.Duration(cfg.P2P.PingTimeout) * time.Millisecond,
		IndirectPingCount: cfg.P2P.IndirectPingCount,
		RTTAlpha:          0.25,
	}

	gossip := p2p.NewGossiper(gossipCfg, transport)
	gossip.OnJoin(func(peer *p2p.Peer) {
		log.Printf("P2P: peer %d joined cluster from %s", peer.ID, peer.Addr)
	})
	gossip.OnLeave(func(peerID int32) {
		log.Printf("P2P: peer %d left cluster", peerID)
	})

	queryHandler := NewStorageQueryHandler(store)
	scatterGather := p2p.NewScatterGather(int32(serverId), transport, queryHandler)
	leaseManager := p2p.NewLeaseManager(int32(serverId), transport)

	// Threshold-OPRF provider: this daemon evaluates its own share and gathers
	// peer evaluations over P2P. The evaluator never logs or persists the
	// blinded tag.
	var oprfProvider *p2p.OPRFProvider
	if cfg.Global.OPRFEnabled {
		evaluator, oErr := newOPRFShareEvaluator(daemons[serverId])
		if oErr != nil {
			log.Printf("P2P: OPRF evaluator not configured: %v", oErr)
			return nil, nil, nil
		}
		oprfProvider = p2p.NewOPRFProvider(int32(serverId), transport, evaluator)
	}

	gossip.SetScatterGather(scatterGather)
	gossip.SetLeaseManager(leaseManager)
	if oprfProvider != nil {
		gossip.SetOPRFProvider(oprfProvider)
	}

	for i, d := range daemons {
		if i == serverId {
			continue
		}
		dHost, _, _ := net.SplitHostPort(d.Host)
		peerPort := basePort + i
		peerAddr := net.JoinHostPort(dHost, strconv.Itoa(peerPort))
		if _, err := transport.Dial(int32(i), peerAddr); err != nil {
			log.Printf("P2P: failed to dial bootstrap peer %d at %s: %v", i, peerAddr, err)
		}
	}

	leaseManager.Start()
	gossip.Run()

	log.Printf("P2P: gossip started, node %d, %d peers connected", serverId, transport.Peers().Count())

	go func() {
		<-ctx.Done()
		leaseManager.Stop()
		gossip.Close()
		transport.Close()
	}()

	return scatterGather, leaseManager, oprfProvider
}

// downgradeToServerSideMode finds the next mode in replicationOrder that is NOT
// in clientSideModes, starting from the position after the current mode.
// If the current mode is already server-side (not in clientSideModes), it is
// returned as-is. If no suitable mode is found, falls back to ReplicationNone (0)
// to ensure the file is at least stored locally.
func downgradeToServerSideMode(currentMode int, replicationOrder []int, clientSideModes []int) int {
	isClientSide := func(mode int) bool {
		for _, csm := range clientSideModes {
			if mode == csm {
				return true
			}
		}
		return false
	}

	// If the current mode is already server-side, no downgrade needed.
	if !isClientSide(currentMode) {
		return currentMode
	}

	// Find the position of currentMode in replicationOrder and walk forward.
	startIdx := 0
	for i, mode := range replicationOrder {
		if mode == currentMode {
			startIdx = i + 1
			break
		}
	}

	for i := startIdx; i < len(replicationOrder); i++ {
		if !isClientSide(replicationOrder[i]) {
			return replicationOrder[i]
		}
	}

	// No server-side mode found after current position — scan from the beginning.
	for i := 0; i < startIdx && i < len(replicationOrder); i++ {
		if !isClientSide(replicationOrder[i]) {
			return replicationOrder[i]
		}
	}

	return common.ReplicationNone
}
