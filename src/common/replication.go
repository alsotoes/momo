package common

import (
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Connect establishes connections with daemon(s) and sends a file.
// It first connects to a specified daemon to determine the replication mode.
// If splay replication is active, it connects to all other daemons.
// Finally, it sends the file to all established connections concurrently.
func Connect(wg *sync.WaitGroup, daemons []*Daemon, filePath string, serverId int, timestamp int64) {
	defer wg.Done()
	var connections []net.Conn
	var wgSendFile sync.WaitGroup

	// Connect to the initial daemon to check replication mode
	initialConn, err := DialSocket(daemons[serverId].Host)
	if err != nil {
		log.Printf("Failed to connect to initial daemon %s: %v", daemons[serverId].Host, err)
		return
	}
	connections = append(connections, initialConn)

	// Perform handshake to get replication mode
	if _, err := initialConn.Write([]byte(strconv.FormatInt(timestamp, 10))); err != nil {
		log.Printf("Failed to send timestamp to %s: %v", daemons[serverId].Host, err)
		initialConn.Close()
		return
	}
	bufferReplicationMode := make([]byte, 1)
	if _, err := io.ReadFull(initialConn, bufferReplicationMode); err != nil {
		log.Printf("Failed to read replication mode from %s: %v", daemons[serverId].Host, err)
		initialConn.Close()
		return
	}
	log.Printf("Client replicationMode: %s", string(bufferReplicationMode))

	replicationMode, err := strconv.Atoi(string(bufferReplicationMode))
	if err != nil {
		log.Printf("Invalid replication mode received from %s: %v", daemons[serverId].Host, err)
		initialConn.Close()
		return
	}

	if replicationMode == ReplicationPrimarySplay {
		// In splay replication, connect to all other daemons as well
		for i, daemon := range daemons {
			if i == serverId {
				continue // Already connected
			}

			conn, err := DialSocket(daemon.Host)
			if err != nil {
				log.Printf("Failed to connect to daemon %s: %v", daemon.Host, err)
				continue
			}

			// Perform handshake with the other daemons
			if _, err := conn.Write([]byte(strconv.FormatInt(timestamp, 10))); err != nil {
				log.Printf("Failed to send timestamp to %s: %v", daemon.Host, err)
				conn.Close()
				continue
			}
			dummyBuffer := make([]byte, 1)
			if _, err := io.ReadFull(conn, dummyBuffer); err != nil {
				log.Printf("Failed to complete handshake with %s: %v", daemon.Host, err)
				conn.Close()
				continue
			}

			connections = append(connections, conn)
		}
	}

	// Close all connections at the end
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()

	// Send the file to all established connections concurrently
	wgSendFile.Add(len(connections))
	for _, conn := range connections {
		go sendFile(&wgSendFile, conn, filePath)
	}
	wgSendFile.Wait()
}

// hashLength is the expected length of a SHA-256 hash string.
const hashLength = 64

// sendFile sends a file over a network connection.
// It first sends the file's metadata (SHA-256 hash, name, and size) and then the file's content.
// It waits for an acknowledgment ("ACK") from the server upon successful reception.
func sendFile(wg *sync.WaitGroup, connection net.Conn, fileName string) {
	defer wg.Done()

	file, err := os.Open(fileName)
	if err != nil {
		log.Printf("Failed to open file %s: %v", fileName, err)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Printf("Failed to get file info for %s: %v", fileName, err)
		return
	}

	fileSize := fileInfo.Size()
	fileHash, err := HashFile(fileName)
	if err != nil {
		log.Printf("Failed to hash file %s: %v", fileName, err)
		return
	}

	log.Printf("=> Hash:    %s", fileHash)
	log.Printf("=> Name:    %s", fileInfo.Name())
	log.Printf("=> Size:    %d", fileSize)

	// Send metadata
	// Optimization: Pre-allocate a single buffer for the exact packet size.
	// This avoids multiple string formatting allocations and multiple system calls.
	metadataBuffer := make([]byte, hashLength+FileInfoLength+FileInfoLength)

	copy(metadataBuffer[0:hashLength], fileHash)
	copy(metadataBuffer[hashLength:hashLength+FileInfoLength], padString(fileInfo.Name(), FileInfoLength))

	// Format size directly into the buffer avoiding fmt.Sprintf
	sizeBytes := strconv.AppendInt(make([]byte, 0, FileInfoLength), fileSize, 10)
	copy(metadataBuffer[hashLength+FileInfoLength:], sizeBytes)

	connection.Write(metadataBuffer)

	// Send file content
	// Optimization: Use io.Copy to avoid manual buffer allocation and read/write loops.
	// This can leverage kernel-level zero-copy optimizations (e.g., sendfile).
	if _, err := io.Copy(connection, file); err != nil {
		log.Printf("Error sending file %s: %v", fileName, err)
		return
	}

	// Wait for ACK
	ackBuffer := make([]byte, 3)
	if _, err := io.ReadFull(connection, ackBuffer); err != nil {
		log.Printf("Failed to read ACK from server: %v", err)
		return
	}

	if string(ackBuffer) != "ACK" {
		log.Printf("Received unexpected response from server: %s", string(ackBuffer))
		return
	}

	log.Printf("File %s sent successfully.", fileName)
}

// padString pads a string with null characters to a specified length.
// If the string is longer than the specified length, it is truncated.
func padString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	// ⚡ Bolt: Use strings.Repeat for a simpler, faster, and more readable optimization.
	// This reduces memory allocations compared to string(make([]byte, n)) while maintaining clarity.
	return input + strings.Repeat("\x00", length-len(input))
}
