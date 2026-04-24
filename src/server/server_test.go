// Package server provides the core functionality for the momo server.
package server

import (
	"crypto/subtle"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// mockConnect is a mock implementation of Connect for testing.
func mockConnect(wg *sync.WaitGroup, cfg momo_common.Configuration, filePath string, serverId int, timestamp int64) {
	defer wg.Done()
	// In a real test, you might add more logic here to simulate the client's behavior
}

// mockGetFile is a mock implementation of getFile for testing.
func mockGetFile(connection net.Conn, path string, fileName string, expectedHash string, fileSize int64) error {
	// This mock function will consume exactly fileSize bytes from the connection.
	_, err := io.CopyN(io.Discard, connection, fileSize)
	if err != nil {
		// This can happen if the client closes the connection prematurely.
		// For this test's purpose, we can ignore the error.
	}
	return nil
}

// handleConnection is a testable version of the connection handling logic inside Daemon.
func handleConnection(t *testing.T, connection net.Conn, cfg momo_common.Configuration, serverId int) {
	daemons := cfg.Daemons
	var replicationMode int
	var success bool
	defer func() {
		if success {
			// In a real scenario, this might block if the client isn't reading.
			// net.Pipe is unbuffered, so writes block until a read happens.
			// The client *is* waiting for this ACK, so it should be fine.
			connection.Write([]byte("ACK" + strconv.Itoa(serverId)))
		}
		connection.Close()
	}()

	bufferAuthToken := make([]byte, momo_common.AuthTokenLength)
	if _, err := io.ReadFull(connection, bufferAuthToken); err != nil {
		t.Logf("Error reading AuthToken: %v", err)
		return
	}
	if string(bufferAuthToken) != cfg.Global.AuthToken {
		t.Logf("Invalid AuthToken received")
		return
	}

	bufferTimestamp := make([]byte, momo_common.TimestampLength)
	if _, err := connection.Read(bufferTimestamp); err != nil {
		t.Logf("Error reading timestamp: %v", err)
		return
	}
	timestamp, err := strconv.ParseInt(string(bufferTimestamp), 10, 64)
	if err != nil {
		t.Errorf("Error parsing timestamp: %v", err)
		return
	}

	// The rest of the logic from the original Daemon function's go func() { ... }
	repState := GetReplicationState()
	switch serverId {
	case 0:
		replicationMode = repState.New
	case 1:
		if timestamp > repState.TimeStamp {
			replicationMode = repState.New
		} else {
			replicationMode = repState.Old
		}
		if replicationMode != momo_common.ReplicationChain {
			replicationMode = momo_common.ReplicationNone
		}
	default:
		replicationMode = momo_common.ReplicationNone
	}

	connection.Write([]byte(strconv.Itoa(replicationMode)))

	metadata, err := getMetadata(connection)
	if err != nil {
		t.Logf("Error getting metadata: %v", err)
		return
	}
	var wg sync.WaitGroup

	// Create a temporary directory for the test
	tempDir := t.TempDir()
	daemons[serverId].Data = tempDir

	originalConnect := connectToPeer
	connectToPeer = mockConnect
	defer func() { connectToPeer = originalConnect }()

	switch replicationMode {
	case momo_common.ReplicationNone:
		mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size)
	case momo_common.ReplicationChain:
		if serverId == 1 {
			wg.Add(1)
			mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size)
			connectToPeer(&wg, cfg, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
			wg.Wait()
		} else {
			wg.Add(1)
			mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size)
			connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
			wg.Wait()
		}
	case momo_common.ReplicationSplay:
		wg.Add(2)
		mockGetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.Hash, metadata.Size)
		go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
		go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
		wg.Wait()
	}
	success = true
}

func TestDaemonLogic(t *testing.T) {
	defer goleak.VerifyNone(t)
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

	// The temp file path contains directories. Our code correctly prevents
	// path traversal, so we can't send a full path over the network.
	// We need to just send the base name for the test.
	fileName := filepath.Base(tempFile.Name())
	// But we still need the full path to create the local hash/file. Let's write to it.
	tempFile.Write([]byte(fileContent))
	tempFile.Close()

	hash, _ := momo_common.HashFile(tempFile.Name())

	testCases := []struct {
		name                string
		ReplicationMode     int
		serverId            int
		expectedAck         string
		expectedReplication int
	}{
		{"ReplicationNone", momo_common.ReplicationNone, 0, "ACK0", momo_common.ReplicationNone},
		{"ReplicationSplay", momo_common.ReplicationSplay, 0, "ACK0", momo_common.ReplicationSplay},
		{"ReplicationChain", momo_common.ReplicationChain, 1, "ACK1", momo_common.ReplicationChain},
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	cfg := momo_common.Configuration{
		Daemons: daemons,
		Global: momo_common.ConfigurationGlobal{
			AuthToken: authToken,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			SetReplicationState(tc.ReplicationMode, 0)
			client, server := net.Pipe()

			serverDone := make(chan struct{})
			go func() {
				handleConnection(t, server, cfg, tc.serverId)
				close(serverDone)
			}()

			// Test Execution
			client.Write([]byte(authToken))
			timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)
			client.Write([]byte(timestamp))

			replicationModeBuf := make([]byte, 1)
			client.Read(replicationModeBuf)
			replicationMode, _ := strconv.Atoi(string(replicationModeBuf))

			if replicationMode != tc.expectedReplication {
				t.Errorf("Expected replication mode %d, got %d", tc.expectedReplication, replicationMode)
			}

			// Send metadata
			client.Write([]byte(hash))
			fileNameBytes := make([]byte, momo_common.FileInfoLength)
			copy(fileNameBytes, fileName)
			client.Write(fileNameBytes)
			fileSizeBytes := make([]byte, momo_common.FileInfoLength)
			copy(fileSizeBytes, strconv.Itoa(len(fileContent)))
			client.Write(fileSizeBytes)

			// Send file content
			file, _ := os.Open(tempFile.Name())
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

func TestUnauthenticatedConnection(t *testing.T) {
	defer goleak.VerifyNone(t)
	daemons := []*momo_common.Daemon{{Host: "127.0.0.1:0", Data: ""}}
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	wrongToken := "wrong_token_wrong_token_wrong_token_wrong_token_wrong_token_wro"
	cfg := momo_common.Configuration{
		Daemons: daemons,
		Global: momo_common.ConfigurationGlobal{
			AuthToken: authToken,
		},
	}

	client, server := net.Pipe()
	serverDone := make(chan struct{})
	go func() {
		handleConnection(t, server, cfg, 0)
		close(serverDone)
	}()

	// Try with wrong token
	client.Write([]byte(wrongToken))

	// Attempt to read something (e.g., replication mode) - should fail because server closes connection
	replicationModeBuf := make([]byte, 1)
	client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, err := client.Read(replicationModeBuf)
	if err == nil {
		t.Error("Expected error (connection closed) when using wrong AuthToken, but read was successful")
	}

	client.Close()
	<-serverDone
}
