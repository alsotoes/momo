// Package server provides the core functionality for the momo server.
package server

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// mockConnect is a mock implementation of momo_client.Connect for testing.
func mockConnect(wg *sync.WaitGroup, daemons []*momo_common.Daemon, filename string, serverId int, timestamp int64) {
	defer wg.Done()
	// In a real test, you might add more logic here to simulate the client's behavior
}

// mockGetFile is a mock implementation of getFile for testing.
func mockGetFile(connection net.Conn, path string, fileName string, fileMD5 string, fileSize int64) {
	// This mock function will consume exactly fileSize bytes from the connection.
	_, err := io.CopyN(io.Discard, connection, fileSize)
	if err != nil {
		// This can happen if the client closes the connection prematurely.
		// For this test's purpose, we can ignore the error.
	}
}

// handleConnection is a testable version of the connection handling logic inside Daemon.
func handleConnection(t *testing.T, connection net.Conn, daemons []*momo_common.Daemon, serverId int) {
	var replicationMode int
	defer func() {
		// In a real scenario, this might block if the client isn't reading.
		// net.Pipe is unbuffered, so writes block until a read happens.
		// The client *is* waiting for this ACK, so it should be fine.
		connection.Write([]byte("ACK" + strconv.Itoa(serverId)))
		connection.Close()
	}()

	bufferTimestamp := make([]byte, momo_common.TimestampLength)
	connection.Read(bufferTimestamp)
	timestamp, err := strconv.ParseInt(string(bufferTimestamp), 10, 64)
	if err != nil {
		t.Fatalf("Error parsing timestamp: %v", err)
	}

	// The rest of the logic from the original Daemon function's go func() { ... }
	switch serverId {
	case 0:
		replicationMode = ReplicationState.New
	case 1:
		if timestamp > ReplicationState.TimeStamp {
			replicationMode = ReplicationState.New
		} else {
			replicationMode = ReplicationState.Old
		}
		if replicationMode != momo_common.ReplicationChain {
			replicationMode = momo_common.ReplicationNone
		}
	default:
		replicationMode = momo_common.ReplicationNone
	}

	connection.Write([]byte(strconv.Itoa(replicationMode)))

	metadata := getMetadata(connection)
	var wg sync.WaitGroup

	// Create a temporary directory for the test
	tempDir := t.TempDir()
	daemons[serverId].Data = tempDir

	originalConnect := connectToPeer
	connectToPeer = mockConnect
	defer func() { connectToPeer = originalConnect }()

	switch replicationMode {
	case momo_common.ReplicationNone:
		mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
	case momo_common.ReplicationChain:
		if serverId == 1 {
			wg.Add(1)
			mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
			connectToPeer(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
			wg.Wait()
		} else {
			wg.Add(1)
			mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
			connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
			wg.Wait()
		}
	case momo_common.ReplicationSplay:
		wg.Add(2)
		mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
		go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
		go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
		wg.Wait()
	}
}

func TestDaemonLogic(t *testing.T) {
	// Setup common test data
	daemons := []*momo_common.Daemon{
		{Host: "127.0.0.1:0", Data: ""},
		{Host: "127.0.0.1:0", Data: ""},
		{Host: "127.0.0.1:0", Data: ""},
	}

	fileContent := "hello world"
	tempFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	fileName := tempFile.Name()
	tempFile.Write([]byte(fileContent))
	tempFile.Close()

	md5, _ := momo_common.HashFile(fileName)

	testCases := []struct {
		name              string
		ReplicationMode   int
		serverId          int
		expectedAck       string
		expectedReplication int
	}{
		{"ReplicationNone", momo_common.ReplicationNone, 0, "ACK0", momo_common.ReplicationNone},
		{"ReplicationSplay", momo_common.ReplicationSplay, 0, "ACK0", momo_common.ReplicationSplay},
		{"ReplicationChain", momo_common.ReplicationChain, 1, "ACK1", momo_common.ReplicationChain},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ReplicationState.New = tc.ReplicationMode
			client, server := net.Pipe()

			serverDone := make(chan struct{})
			go func() {
				handleConnection(t, server, daemons, tc.serverId)
				close(serverDone)
			}()

			// Test Execution
			timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)
			client.Write([]byte(timestamp))

			replicationModeBuf := make([]byte, 1)
			client.Read(replicationModeBuf)
			replicationMode, _ := strconv.Atoi(string(replicationModeBuf))

			if replicationMode != tc.expectedReplication {
				t.Errorf("Expected replication mode %d, got %d", tc.expectedReplication, replicationMode)
			}

			// Send metadata
			client.Write([]byte(md5))
			fileNameBytes := make([]byte, momo_common.FileInfoLength)
			copy(fileNameBytes, fileName)
			client.Write(fileNameBytes)
			fileSizeBytes := make([]byte, momo_common.FileInfoLength)
			copy(fileSizeBytes, strconv.Itoa(len(fileContent)))
			client.Write(fileSizeBytes)

			// Send file content
			file, _ := os.Open(fileName)
			filedata := make([]byte, len(fileContent))
			file.Read(filedata)
			client.Write(filedata)

			ackBuf := make([]byte, len(tc.expectedAck))
			client.Read(ackBuf)

			if string(ackBuf) != tc.expectedAck {
				t.Errorf("Expected %s, got %s", tc.expectedAck, string(ackBuf))
			}

			client.Close()

			select {
			case <-serverDone:
			case <-time.After(2 * time.Second):
				t.Fatal("Test timed out, server goroutine is likely deadlocked.")
			}
		})
	}
}
