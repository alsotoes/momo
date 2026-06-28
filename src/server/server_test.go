// Package server provides the core functionality for the momo server.
package server

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// mockConnect is a mock implementation of Connect for testing.
func mockConnect(wg *sync.WaitGroup, cfg common.Configuration, filePath string, remotePath string, serverId int, timestamp int64, requestedMode int, replicationFactor int) {
	defer wg.Done()
	// In a real test, you might add more logic here to simulate the client's behavior
}

// mockGetFile is a mock implementation of getFile for testing.
func mockGetFile(comm transport.Communicator, store storage.Store, fileName string, expectedHash string, fileSize int64) error {
	// This mock function will consume exactly fileSize bytes from the connection.
	_, err := io.CopyN(io.Discard, comm, fileSize)
	if err != nil {
		// This can happen if the client closes the connection prematurely.
		// For this test's purpose, we can ignore the error.
	}
	return nil
}

// handleConnection is a testable version of the connection handling logic inside Daemon.
func handleConnection(t *testing.T, connection net.Conn, cfg common.Configuration, serverId int) {
	daemons := cfg.Daemons
	var replicationMode int
	var success bool
	comm := transport.NewMomoTCPCommunicator(connection)
	defer func() {
		if success {
			comm.SendACK(serverId)
		}
		comm.Close()
	}()

	expectedAuthToken := []byte(common.PadString(cfg.Global.AuthToken, common.AuthTokenLength))
	
	// HandshakeServer performs the server-side handshake: receives AuthToken + Timestamp + RequestedMode
	replicationMode, timestamp, err := comm.HandshakeServer(expectedAuthToken)
	if err != nil {
		t.Logf("Handshake failed: %v", err)
		return
	}

	// The rest of the logic from the original Daemon function
	repState := GetReplicationState()
	// If it's a direct client connection (timestamp == 0 in this simplified test), use local state.
	// In the real Daemon, we use common.DummyEpoch.
	if timestamp == 0 || timestamp == common.DummyEpoch {
		replicationMode = repState.New
	}

	comm.SendReplicationMode(replicationMode)

	metadata, err := comm.ReceiveMetadata()
	if err != nil {
		t.Logf("Error getting metadata: %v", err)
		return
	}

	// Send metadata status
	comm.SendMetadataStatus(transport.MetadataStatusSendPayload)

	var wg sync.WaitGroup

	// Create a temporary directory for the test
	tempDir := t.TempDir()
	daemons[serverId].Data = tempDir
	store, err := storage.NewCASStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create CAS store: %v", err)
	}
	defer store.Close()

	originalConnect := connectToPeer
	connectToPeer = mockConnect
	defer func() { connectToPeer = originalConnect }()

	switch replicationMode {
	case common.ReplicationNone:
		mockGetFile(comm, store, metadata.Name, metadata.Hash, metadata.Size)
	case common.ReplicationChain:
		if serverId == 1 {
			wg.Add(1)
			mockGetFile(comm, store, metadata.Name, metadata.Hash, metadata.Size)
			connectToPeer(&wg, cfg, daemons[1].Data+"/"+metadata.Name, "", 2, timestamp, 0, cfg.Global.ReplicationFactor)
			wg.Wait()
		} else {
			wg.Add(1)
			mockGetFile(comm, store, metadata.Name, metadata.Hash, metadata.Size)
			connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, "", 1, timestamp, 0, cfg.Global.ReplicationFactor)
			wg.Wait()
		}
	case common.ReplicationSplay:
		wg.Add(2)
		mockGetFile(comm, store, metadata.Name, metadata.Hash, metadata.Size)
		go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, "", 1, timestamp, 0, cfg.Global.ReplicationFactor)
		go connectToPeer(&wg, cfg, daemons[0].Data+"/"+metadata.Name, "", 2, timestamp, 0, cfg.Global.ReplicationFactor)
		wg.Wait()
	}
	success = true
}

func TestDaemonLogic(t *testing.T) {
	// Setup common test data
	daemons := []*common.Daemon{
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

	hash, _ := common.HashFile(tempFile.Name())

	testCases := []struct {
		name                string
		ReplicationMode     int
		serverId            int
		expectedAck         string
		expectedReplication int
	}{
		{"ReplicationNone", common.ReplicationNone, 0, "ACK0", common.ReplicationNone},
		{"ReplicationSplay", common.ReplicationSplay, 0, "ACK0", common.ReplicationSplay},
		{"ReplicationChain", common.ReplicationChain, 1, "ACK1", common.ReplicationChain},
	}

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
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
			client.Write([]byte(common.PadString(authToken, common.AuthTokenLength)))
			// ⚡ Bolt: Use DummyEpoch to signal that we are the primary in this test
			timestamp := strconv.FormatInt(common.DummyEpoch, 10)
			client.Write([]byte(timestamp))
			// ⚡ Bolt: Send the 84th byte (RequestedMode = 0)
			client.Write([]byte("0"))

			replicationModeBuf := make([]byte, 1)
			client.Read(replicationModeBuf)
			replicationMode, _ := strconv.Atoi(string(replicationModeBuf))

			if replicationMode != tc.expectedReplication {
				t.Errorf("Expected replication mode %d, got %d", tc.expectedReplication, replicationMode)
			}

			// Send metadata
			client.Write([]byte(hash))
			fileNameBytes := make([]byte, common.FileInfoLength)
			copy(fileNameBytes, fileName)
			client.Write(fileNameBytes)
			fileSizeBytes := make([]byte, common.FileInfoLength)
			copy(fileSizeBytes, strconv.Itoa(len(fileContent)))
			client.Write(fileSizeBytes)

			// ⚡ Bolt: Read the Metadata Status byte
			statusBuf := make([]byte, 1)
			client.Read(statusBuf)
			if statusBuf[0] != transport.MetadataStatusSendPayload {
				t.Errorf("Expected status %d, got %d", transport.MetadataStatusSendPayload, statusBuf[0])
			}

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
	daemons := []*common.Daemon{{Host: "127.0.0.1:0", Data: ""}}
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // not a real token
	wrongToken := "wrong_token_wrong_token_wrong_token_wrong_token_wrong_token_wro"
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
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
	client.Write([]byte(common.PadString(wrongToken, common.AuthTokenLength)))

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
