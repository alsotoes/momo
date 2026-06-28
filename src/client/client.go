package client

import (
	"io"
	"log"
	"os"
	"sync"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// Connect establishes connections with daemon(s) and sends a file.
// It first connects to a specified daemon to determine the replication mode.
// If splay replication is active, it connects to all other daemons.
// Finally, it sends the file to all established connections concurrently.
func Connect(wg *sync.WaitGroup, cfg common.Configuration, filePath string, remotePath string, serverId int, timestamp int64, requestedMode int, replicationFactor int) {
	defer wg.Done()
	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		log.Printf("Server ID %d is out of range", serverId)
		return
	}
	authToken := cfg.Global.AuthToken
	factory := transport.NewProtocolFactory(cfg)
	var communicators []transport.Communicator
	var wgSendFile sync.WaitGroup

	// Connect to the initial daemon to check replication mode
	comm, err := factory.Dial(daemons[serverId].Host)
	if err != nil {
		log.Printf("Failed to connect to initial daemon %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		return
	}
	communicators = append(communicators, comm)

	// Perform handshake to get replication mode
	replicationMode, err := comm.HandshakeClient(authToken, timestamp, requestedMode)
	if err != nil {
		log.Printf("Handshake failed with %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		comm.Close()
		return
	}

	if replicationMode == common.ReplicationPrimarySplay {
		// ⚡ Bolt: Use CRUSH to find the specific replicas for PrimarySplay.
		fileHash, _ := common.HashFile(filePath)
		nodes := make([]*common.Node, len(daemons))
		for i, d := range daemons {
			nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host}
		}
		cmap := &common.ClusterMap{Nodes: nodes}
		placement, _ := cmap.Placement(fileHash, replicationFactor)

		for _, node := range placement {
			if node.ID == serverId {
				continue // Already connected
			}

			peerComm, err := factory.Dial(node.Addr)
			if err != nil {
				log.Printf("Failed to connect to daemon %s: %v", node.Addr, common.SanitizeLog(err.Error()))
				continue
			}

			// Perform handshake with the other daemons
			if _, err := peerComm.HandshakeClient(authToken, timestamp, replicationMode); err != nil {
				log.Printf("Handshake failed with peer %s: %v", node.Addr, common.SanitizeLog(err.Error()))
				peerComm.Close()
				continue
			}

			communicators = append(communicators, peerComm)
		}
	}

	// Close all communicators at the end
	defer func() {
		for _, c := range communicators {
			c.Close()
		}
	}()

	// Optimization: Pre-compute file metadata (hash, size, name) before concurrent transmission.
	// This avoids redundant disk reads and hashing for each connection in splay replication.
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Printf("Failed to get file info for %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
		return
	}

	fileHash, err := common.HashFile(filePath)
	if err != nil {
		log.Printf("Failed to hash file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
		return
	}

	meta := &common.FileMetadata{
		Name:       fileInfo.Name(),
		Hash:       fileHash,
		Size:       fileInfo.Size(),
		RemotePath: remotePath,
	}

	// Validate RemotePath and length limit before transmission
	wireName := meta.Name
	if meta.RemotePath != "" {
		normalized, err := common.NormalizeVirtualPath(meta.RemotePath)
		if err != nil {
			log.Printf("Failed to upload %s: invalid remote path %q: %v", common.SanitizeLog(filePath), common.SanitizeLog(meta.RemotePath), err)
			return
		}
		wireName = normalized + "/" + meta.Name
	}
	if len(wireName) > common.FileInfoLength {
		log.Printf("Failed to upload %s: remote path and filename exceed limit of %d characters", common.SanitizeLog(filePath), common.FileInfoLength)
		return
	}

	log.Printf("=> Hash:    %s", common.SanitizeLog(meta.Hash))
	log.Printf("=> Name:    %s", common.SanitizeLog(meta.Name))
	log.Printf("=> Size:    %d", meta.Size)

	// Send the file to all established connections concurrently
	wgSendFile.Add(len(communicators))
	for _, c := range communicators {
		go sendFile(&wgSendFile, c, filePath, meta)
	}
	wgSendFile.Wait()
}

// sendFile sends a file over a network connection.
// It first sends the file's metadata (SHA-256 hash, name, and size) and then the file's content.
// It waits for an acknowledgment ("ACK") from the server upon successful reception.
func sendFile(wg *sync.WaitGroup, comm transport.Communicator, filePath string, meta *common.FileMetadata) {
	defer wg.Done()
	// 🛡️ Zero-Crash: Ensure background transmission tasks don't crash the client on unexpected panics
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in sendFile for %s: %v", common.SanitizeLog(filePath), r)
		}
	}()

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Failed to open file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
		return
	}

	defer file.Close()

	// Send metadata and receive status
	status, err := comm.SendMetadata(meta)
	if err != nil {
		log.Printf("Failed to send metadata for %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
		return
	}

	// ⚡ Bolt: Handle deduplication shortcut.
	if status == transport.MetadataStatusSkipPayload {
		log.Printf("Server already has content for %s, skipping upload.", common.SanitizeLog(meta.Name))
	} else {
		// Send file content
		if _, err := io.Copy(comm, file); err != nil {
			log.Printf("Error sending file %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
			return
		}
	}

	// Wait for ACK
	if err := comm.ReceiveACK(); err != nil {
		log.Printf("Failed to read ACK from server: %v", common.SanitizeLog(err.Error()))
		return
	}

	log.Printf("File %s sent successfully.", common.SanitizeLog(meta.Name))
}
