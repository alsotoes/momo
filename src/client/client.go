package client

import (
	"errors"
	"log"
	"net"
	"strconv"
	"sync"

	momo_common "github.com/alsotoes/momo/src/common"
)

// Connect establishes connections with daemon(s) and sends a file.
// It first connects to a specified daemon to determine the replication mode.
// If splay replication is active, it connects to all other daemons.
// Finally, it sends the file to all established connections concurrently.
func Connect(daemons []*momo_common.Daemon, filePath string, serverId int, timestamp int64) {
	var connections []net.Conn
	var wgSendFile sync.WaitGroup

	// Connect to the initial daemon to check replication mode
	initialConn, err := dialSocket(daemons[serverId].Host)
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
	if _, err := initialConn.Read(bufferReplicationMode); err != nil {
		log.Printf("Failed to read replication mode from %s: %v", daemons[serverId].Host, err)
		initialConn.Close()
		return
	}
	log.Printf("Client replicationMode: " + string(bufferReplicationMode))

	replicationMode, err := strconv.Atoi(string(bufferReplicationMode))
	if err != nil {
		log.Printf("Invalid replication mode received from %s: %v", daemons[serverId].Host, err)
		initialConn.Close()
		return
	}

	if replicationMode == momo_common.PRIMARY_SPLAY_REPLICATION {
		// In splay replication, connect to all other daemons as well
		for i, daemon := range daemons {
			if i == serverId {
				continue // Already connected
			}

			conn, err := dialSocket(daemon.Host)
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
			if _, err := conn.Read(dummyBuffer); err != nil {
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

// dialSocket connects to the given address.
// It returns a net.Conn or an error.
func dialSocket(servAddr string) (net.Conn, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", servAddr)
	if err != nil {
		return nil, errors.New("ResolveTCPAddr failed: " + err.Error())
	}

	connection, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, errors.New("Dial failed: " + err.Error())
	}

	return connection, nil
}
