package common

import (
	"io"
	"log"
	"os"
	"sync"
)

// Connect establishes connections with daemon(s) and sends a file.
// It first connects to a specified daemon to determine the replication mode.
// If splay replication is active, it connects to all other daemons.
// Finally, it sends the file to all established connections concurrently.
func Connect(wg *sync.WaitGroup, cfg Configuration, filePath string, serverId int, timestamp int64) {
	defer wg.Done()
	daemons := cfg.Daemons
	authToken := cfg.Global.AuthToken
	factory := NewProtocolFactory(cfg)
	var communicators []Communicator
	var wgSendFile sync.WaitGroup

	// Connect to the initial daemon to check replication mode
	comm, err := factory.Dial(daemons[serverId].Host)
	if err != nil {
		log.Printf("Failed to connect to initial daemon %s: %v", daemons[serverId].Host, SanitizeLog(err.Error()))
		return
	}
	communicators = append(communicators, comm)

	// Perform handshake to get replication mode
	replicationMode, err := comm.HandshakeClient(authToken, timestamp)
	if err != nil {
		log.Printf("Handshake failed with %s: %v", daemons[serverId].Host, SanitizeLog(err.Error()))
		comm.Close()
		return
	}

	if replicationMode == ReplicationPrimarySplay {
		// In splay replication, connect to all other daemons as well
		for i, daemon := range daemons {
			if i == serverId {
				continue // Already connected
			}

			peerComm, err := factory.Dial(daemon.Host)
			if err != nil {
				log.Printf("Failed to connect to daemon %s: %v", daemon.Host, SanitizeLog(err.Error()))
				continue
			}

			// Perform handshake with the other daemons
			if _, err := peerComm.HandshakeClient(authToken, timestamp); err != nil {
				log.Printf("Handshake failed with peer %s: %v", daemon.Host, SanitizeLog(err.Error()))
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
		log.Printf("Failed to get file info for %s: %v", SanitizeLog(filePath), SanitizeLog(err.Error()))
		return
	}

	fileHash, err := HashFile(filePath)
	if err != nil {
		log.Printf("Failed to hash file %s: %v", SanitizeLog(filePath), SanitizeLog(err.Error()))
		return
	}

	meta := &FileMetadata{
		Name: fileInfo.Name(),
		Hash: fileHash,
		Size: fileInfo.Size(),
	}

	log.Printf("=> Hash:    %s", SanitizeLog(meta.Hash))
	log.Printf("=> Name:    %s", SanitizeLog(meta.Name))
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
func sendFile(wg *sync.WaitGroup, comm Communicator, filePath string, meta *FileMetadata) {
	defer wg.Done()

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Failed to open file %s: %v", SanitizeLog(filePath), SanitizeLog(err.Error()))
		return
	}

	defer file.Close()

	// Send metadata
	if err := comm.SendMetadata(meta); err != nil {
		log.Printf("Failed to send metadata for %s: %v", SanitizeLog(meta.Name), SanitizeLog(err.Error()))
		return
	}

	// Send file content
	if _, err := io.Copy(comm, file); err != nil {
		log.Printf("Error sending file %s: %v", SanitizeLog(meta.Name), SanitizeLog(err.Error()))
		return
	}

	// Wait for ACK
	if err := comm.ReceiveACK(); err != nil {
		log.Printf("Failed to read ACK from server: %v", SanitizeLog(err.Error()))
		return
	}

	log.Printf("File %s sent successfully.", SanitizeLog(meta.Name))
}

// PadString pads a string with null characters to a specified length.
// If the string is longer than the specified length, it is truncated.
func PadString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	b := make([]byte, length)
	copy(b, input)
	return string(b)
}
